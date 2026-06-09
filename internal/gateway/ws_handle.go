package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/redis/go-redis/v9"
	authpb "github.com/xh1126xx/gochatx/api"
	"github.com/xh1126xx/gochatx/internal/storage"
	"google.golang.org/grpc"
)

// WSMsg represents a client-to-server JSON message.
type WSMsg struct {
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	RoomID  string `json:"room_id,omitempty"`
	To      string `json:"to,omitempty"`
	Content string `json:"content,omitempty"`
	Limit   int64  `json:"limit,omitempty"`
}

// WSPayload represents a server-to-client JSON message.
type WSPayload struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// GatewayServer holds dependencies for WebSocket and REST handlers.
type GatewayServer struct {
	AuthClient  authpb.AuthServiceClient
	Mongo       *storage.MangoStore
	Redis       *redis.Client
	CheckOrigin func(r *http.Request) bool
}

// NewGatewayServer constructs a GatewayServer.
func NewGatewayServer(authConn *grpc.ClientConn, mongo *storage.MangoStore, rdb *redis.Client) *GatewayServer {
	var authClient authpb.AuthServiceClient
	if authConn != nil {
		authClient = authpb.NewAuthServiceClient(authConn)
	}
	return &GatewayServer{
		AuthClient:  authClient,
		Mongo:       mongo,
		Redis:       rdb,
		CheckOrigin: func(r *http.Request) bool { return true },
	}
}

// HandleWS handles /ws WebSocket connections.
func (s *GatewayServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     s.CheckOrigin,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	// Pong handler resets read deadline
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	sendCh := make(chan []byte, 256)
	defer close(sendCh)

	// Write goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for bs := range sendCh {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, bs); err != nil {
				return
			}
		}
	}()

	// Server-side ping goroutine
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	var me *Client
	authAttempts := 0
	const maxAuthAttempts = 3

	// Set initial read deadline
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Read loop
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m WSMsg
		if err := json.Unmarshal(data, &m); err != nil {
			sendJSON(sendCh, WSPayload{Type: "error", Data: "invalid message format"})
			continue
		}

		switch m.Type {
		case "auth":
			if s.AuthClient == nil {
				// Dev mode: use token as userID
				userID := m.Token
				if userID == "" {
					sendJSON(sendCh, WSPayload{Type: "error", Data: "token required"})
					continue
				}
				me = &Client{UserID: userID, Conn: conn, Send: sendCh}
				registerOnline(userID, me)
				s.pushUndelivered(userID)
				if s.Redis != nil {
					_ = s.Redis.Set(context.Background(), "user:"+userID+":online", "1", 24*time.Hour).Err()
					_ = s.Redis.Set(context.Background(), "user:"+userID+":last_seen", fmt.Sprintf("%d", time.Now().UnixMilli()), 0).Err()
				}
				sendJSON(sendCh, WSPayload{Type: "authed", Data: map[string]string{"user": userID}})
				continue
			}

			authAttempts++
			if authAttempts > maxAuthAttempts {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "too many auth attempts"})
				conn.Close()
				break
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			resp, err := s.AuthClient.Validate(ctx, &authpb.TokenRequest{Token: m.Token})
			cancel()
			if err != nil || resp == nil || !resp.Valid {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "auth failed"})
				continue
			}
			userID := resp.UserId
			me = &Client{UserID: userID, Conn: conn, Send: sendCh}
			registerOnline(userID, me)
			s.pushUndelivered(userID)
			if s.Redis != nil {
				_ = s.Redis.Set(context.Background(), "user:"+userID+":online", "1", 0).Err()
				_ = s.Redis.Set(context.Background(), "user:"+userID+":last_seen", fmt.Sprintf("%d", time.Now().UnixMilli()), 0).Err()
			}
			sendJSON(sendCh, WSPayload{Type: "authed", Data: map[string]string{"user": userID}})

		case "join":
			if me == nil {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "未认证"})
				continue
			}
			room := m.RoomID
			r := getOrCreateRoom(room)
			r.addClient(me)
			his := []*storage.ChatMessage{}
			if s.Mongo != nil {
				if msgs, err := s.Mongo.QueryHistory(room, 50); err == nil {
					his = msgs
				}
			}
			sendJSON(sendCh, WSPayload{Type: "joined", Data: map[string]any{"room": room, "history": his}})

		case "leave":
			if me == nil {
				continue
			}
			room := m.RoomID
			r := getOrCreateRoom(room)
			r.removeClient(me)
			removeEmptyRoom(room)
			sendJSON(sendCh, WSPayload{Type: "left", Data: room})

		case "send":
			if me == nil {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "未认证"})
				continue
			}
			room := m.RoomID
			payload := map[string]any{
				"type": "message",
				"from": me.UserID,
				"room": room,
				"ts":   time.Now().UnixMilli(),
				"msg":  m.Content,
			}
			bs, err := json.Marshal(payload)
			if err != nil {
				log.Printf("ERROR: marshal send message: %v", err)
				sendJSON(sendCh, WSPayload{Type: "error", Data: "failed to encode message"})
				continue
			}
			r := getOrCreateRoom(room)
			r.broadcast(bs)
			if s.Mongo != nil {
				cm := &storage.ChatMessage{
					ID:        fmt.Sprintf("%d_%s_%04d", time.Now().UnixMilli(), me.UserID, rand.Intn(10000)),
					From:      me.UserID,
					RoomID:    room,
					Content:   m.Content,
					Timestamp: time.Now(),
					Delivered: true,
				}
				go func() {
					if err := s.Mongo.SaveMessage(cm); err != nil {
						log.Printf("ERROR: save message: %v", err)
					}
				}()
			}

		case "private":
			if me == nil {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "未认证"})
				continue
			}
			to := m.To
			payload := map[string]any{
				"type": "private",
				"from": me.UserID,
				"to":   to,
				"ts":   time.Now().UnixMilli(),
				"msg":  m.Content,
			}
			bs, err := json.Marshal(payload)
			if err != nil {
				log.Printf("ERROR: marshal private message: %v", err)
				sendJSON(sendCh, WSPayload{Type: "error", Data: "failed to encode message"})
				continue
			}
			if target := getOnlineClient(to); target != nil {
				select {
				case target.Send <- bs:
				default:
				}
				if s.Mongo != nil {
					cm := &storage.ChatMessage{
						ID:        fmt.Sprintf("%d_%s_%s_%04d", time.Now().UnixMilli(), me.UserID, to, rand.Intn(10000)),
						From:      me.UserID,
						To:        to,
						Content:   m.Content,
						Timestamp: time.Now(),
						Delivered: true,
					}
					go func() {
						if err := s.Mongo.SaveMessage(cm); err != nil {
							log.Printf("ERROR: save private message: %v", err)
						}
					}()
				}
			} else {
				if s.Mongo != nil {
					cm := &storage.ChatMessage{
						ID:        fmt.Sprintf("%d_%s_%s_%04d", time.Now().UnixMilli(), me.UserID, to, rand.Intn(10000)),
						From:      me.UserID,
						To:        to,
						Content:   m.Content,
						Timestamp: time.Now(),
						Delivered: false,
					}
					go func() {
						if err := s.Mongo.SaveMessage(cm); err != nil {
							log.Printf("ERROR: save offline message: %v", err)
						}
					}()
				}
			}
			sendJSON(sendCh, WSPayload{Type: "ack", Data: map[string]any{"to": to}})

		case "history":
			if me == nil {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "未认证"})
				continue
			}
			room := m.RoomID
			limit := m.Limit
			if limit <= 0 {
				limit = 50
			}
			var his []*storage.ChatMessage
			if s.Mongo != nil {
				if msgs, err := s.Mongo.QueryHistory(room, limit); err == nil {
					his = msgs
				}
			}
			sendJSON(sendCh, WSPayload{Type: "history", Data: his})

		case "ping":
			sendJSON(sendCh, WSPayload{Type: "pong", Data: time.Now().UnixMilli()})
			if me != nil {
				me.touch()
				if s.Redis != nil {
					_ = s.Redis.Set(context.Background(), "user:"+me.UserID+":last_seen", fmt.Sprintf("%d", me.lastSeen.Load()), 0).Err()
				}
			}

		default:
			sendJSON(sendCh, WSPayload{Type: "error", Data: "未知类型"})
		}
	}

	// Stop ping goroutine
	close(pingDone)

	// Cleanup on disconnect
	if me != nil {
		roomsMu.RLock()
		var myRooms []string
		for id, rm := range rooms {
			rm.mu.RLock()
			_, ok := rm.clients[me.UserID]
			rm.mu.RUnlock()
			if ok {
				myRooms = append(myRooms, id)
			}
		}
		roomsMu.RUnlock()
		for _, rid := range myRooms {
			r := getOrCreateRoom(rid)
			r.removeClient(me)
			removeEmptyRoom(rid)
		}
		unregisterOnline(me.UserID)
		if s.Redis != nil {
			_ = s.Redis.Del(context.Background(), "user:"+me.UserID+":online").Err()
			_ = s.Redis.Set(context.Background(), "user:"+me.UserID+":last_seen", fmt.Sprintf("%d", time.Now().UnixMilli()), 0).Err()
		}
	}

	// Wait for write goroutine — must exceed write timeout (10s)
	select {
	case <-done:
	case <-time.After(12 * time.Second):
	}
}

// pushUndelivered fetches undelivered private messages and pushes them to the user.
func (s *GatewayServer) pushUndelivered(userID string) {
	if s.Mongo == nil {
		return
	}
	msgs, err := s.Mongo.PullUndeliveredForUser(userID)
	if err != nil {
		log.Printf("WARNING: pull undelivered for %s: %v", userID, err)
		return
	}
	if len(msgs) == 0 {
		return
	}
	c := getOnlineClient(userID)
	if c == nil {
		return
	}
	for _, m := range msgs {
		payload := map[string]any{
			"type": "private",
			"from": m.From,
			"to":   userID,
			"ts":   m.Timestamp.UnixMilli(),
			"msg":  m.Content,
		}
		bs, err := json.Marshal(payload)
		if err != nil {
			log.Printf("ERROR: marshal undelivered message: %v", err)
			continue
		}
		select {
		case c.Send <- bs:
		default:
		}
	}
}

// sendJSON marshals v to JSON and sends it to ch. Logs errors instead of silently ignoring.
func sendJSON(ch chan []byte, v interface{}) {
	bs, err := json.Marshal(v)
	if err != nil {
		log.Printf("ERROR: sendJSON marshal: %v", err)
		return
	}
	select {
	case ch <- bs:
	default:
	}
}
