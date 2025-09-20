package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/redis/go-redis/v9"
	authpb "github.com/xh1126xx/gochatx/api"
	"github.com/xh1126xx/gochatx/internal/storage"
	"google.golang.org/grpc"
)

// WS 请求消息结构（JSON）
type WSMsg struct {
	Type    string `json:"type"`              // 消息类型：auth/join/leave/send/private/history/ping
	Token   string `json:"token,omitempty"`   // 认证时携带
	RoomID  string `json:"room_id,omitempty"` // 房间ID
	To      string `json:"to,omitempty"`      // 私聊目标用户ID
	Content string `json:"content,omitempty"` // 文本内容
	Limit   int64  `json:"limit,omitempty"`   // 查询历史消息数量
}

// WS 推送消息结构（JSON）
type WSPayload struct {
	Type string      `json:"type"`           // 消息类型
	Data interface{} `json:"data,omitempty"` // 消息数据
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// GatewayServer 依赖的外部客户端（认证gRPC、Mongo存储、Redis）
type GatewayServer struct {
	AuthClient authpb.AuthServiceClient
	Mongo      *storage.MangoStore
	Redis      *redis.Client
}

// NewGatewayServer 构造函数
func NewGatewayServer(authConn *grpc.ClientConn, mongo *storage.MangoStore, rdb *redis.Client) *GatewayServer {
	var authClient authpb.AuthServiceClient
	if authConn != nil {
		authClient = authpb.NewAuthServiceClient(authConn)
	}
	return &GatewayServer{AuthClient: authClient, Mongo: mongo, Redis: rdb}
}

// HandleWS 处理 /ws WebSocket 连接
func (s *GatewayServer) HandleWS(w http.ResponseWriter, r *http.Request) {
	// 升级为 WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "upgrade failed", http.StatusBadRequest)
		return
	}
	defer conn.Close()

	// 每个连接的发送通道
	sendCh := make(chan []byte, 256)
	defer close(sendCh)

	// 写协程
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

	var me *Client

	// 读循环
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			// 连接关闭
			break
		}
		var m WSMsg
		if err := json.Unmarshal(data, &m); err != nil {
			// 忽略无效消息
			continue
		}

		switch m.Type {
		case "auth":
			// 认证（gRPC）
			if s.AuthClient == nil {
				// 没有认证服务时，直接用 token 作为 userID（开发用）
				userID := m.Token
				me = &Client{UserID: userID, Conn: conn, Send: sendCh}
				registerOnline(userID, me)
				// 推送未送达消息
				s.pushUndelivered(userID)
				// Redis 标记在线
				if s.Redis != nil {
					_ = s.Redis.Set(context.Background(), "user:"+userID+":online", "1", 24*time.Hour).Err()
					_ = s.Redis.Set(context.Background(), "user:"+userID+":last_seen", fmt.Sprintf("%d", time.Now().UnixMilli()), 0).Err()
				}
				sendJSON(sendCh, WSPayload{Type: "authed", Data: map[string]string{"user": userID}})
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			resp, err := s.AuthClient.Validate(ctx, &authpb.TokenRequest{Token: m.Token})
			cancel()
			if err != nil || resp == nil || !resp.Valid {
				sendJSON(sendCh, WSPayload{Type: "error", Data: "auth failed"})
				// 认证失败，连接保持，允许重试
				continue
			}
			userID := resp.UserId
			me = &Client{UserID: userID, Conn: conn, Send: sendCh}
			registerOnline(userID, me)
			// 推送未送达消息
			s.pushUndelivered(userID)
			// Redis 标记在线
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
			// 发送加入确认和最近历史消息
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
			bs, _ := json.Marshal(payload)
			// 广播到房间
			r := getOrCreateRoom(room)
			r.broadcast(bs)
			// 异步持久化消息
			if s.Mongo != nil {
				cm := &storage.ChatMessage{
					ID:        fmt.Sprintf("%d_%s", time.Now().UnixMilli(), me.UserID),
					From:      me.UserID,
					RoomID:    room,
					Content:   m.Content,
					Timestamp: time.Now(),
					Delivered: true,
				}
				go s.Mongo.SaveMessage(cm)
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
			bs, _ := json.Marshal(payload)
			// 尝试发送给在线用户
			if target := getOnlineClient(to); target != nil {
				select {
				case target.Send <- bs:
				default:
					// 对方缓慢，消息丢弃
				}
				// 持久化为已送达
				if s.Mongo != nil {
					cm := &storage.ChatMessage{
						ID:        fmt.Sprintf("%d_%s_%s", time.Now().UnixMilli(), me.UserID, to),
						From:      me.UserID,
						To:        to,
						Content:   m.Content,
						Timestamp: time.Now(),
						Delivered: true,
					}
					go s.Mongo.SaveMessage(cm)
				}
			} else {
				// 对方离线，持久化为未送达，下次登录推送
				if s.Mongo != nil {
					cm := &storage.ChatMessage{
						ID:        fmt.Sprintf("%d_%s_%s", time.Now().UnixMilli(), me.UserID, to),
						From:      me.UserID,
						To:        to,
						Content:   m.Content,
						Timestamp: time.Now(),
						Delivered: false,
					}
					go s.Mongo.SaveMessage(cm)
				}
			}
			// 给发送者确认
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
			// 心跳
			sendJSON(sendCh, WSPayload{Type: "pong", Data: time.Now().UnixMilli()})
			if me != nil {
				me.touch()
				// 刷新 redis 最后活跃时间
				if s.Redis != nil {
					_ = s.Redis.Set(context.Background(), "user:"+me.UserID+":last_seen", fmt.Sprintf("%d", me.lastSeen), 0).Err()
				}
			}

		default:
			sendJSON(sendCh, WSPayload{Type: "error", Data: "未知类型"})
		}
	} // 读循环结束

	// 断开连接时清理
	if me != nil {
		// 从所有房间移除
		roomsMu.RLock()
		var myRooms []string
		for id, rm := range rooms {
			rm.mu.RLock()
			_, ok := rm.Clients[me.UserID]
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
		// 注销在线状态
		unregisterOnline(me.UserID)
		// 更新 redis
		if s.Redis != nil {
			_ = s.Redis.Del(context.Background(), "user:"+me.UserID+":online").Err()
			_ = s.Redis.Set(context.Background(), "user:"+me.UserID+":last_seen", fmt.Sprintf("%d", time.Now().UnixMilli()), 0).Err()
		}
	}
	// 等待写协程退出或超时
	select {
	case <-done:
	case <-time.After(1 * time.Second):
	}
}

// pushUndelivered 查询 Mongo 中 to=userID 且 delivered=false 的消息并推送，然后标记为 delivered=true
func (s *GatewayServer) pushUndelivered(userID string) {
	if s.Mongo == nil {
		return
	}
	msgs, err := s.Mongo.PullUndeliveredForUser(userID)
	if err != nil {
		return
	}
	if len(msgs) == 0 {
		return
	}
	// 尝试推送
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
		bs, _ := json.Marshal(payload)
		select {
		case c.Send <- bs:
		default:
			// 当前无法推送
		}
	}
}

// sendJSON 辅助函数，将数据编码为 JSON 并发送到通道
func sendJSON(ch chan []byte, v interface{}) {
	bs, _ := json.Marshal(v)
	select {
	case ch <- bs:
	default:
		// 如果通道阻塞则丢弃
	}
}
