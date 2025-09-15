package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

type WSMsg struct {
	Type    string `json:"type"`
	Token   string `json:"token,omitempty"`
	RoomID  string `json:"room_id,omitempty"`
	Content string `json:"content,omitempty"`
}

type Client struct {
	UserID string
	Conn   *websocket.Conn
	Send   chan []byte
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, _ := upgrader.Upgrade(w, r, nil)
	defer conn.Close()

	sendCh := make(chan []byte, 64)

	go func() {
		for msg := range sendCh {
			conn.WriteMessage(websocket.TextMessage, msg)

		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg WSMsg
		_ = json.Unmarshal(data, msg)
		switch msg.Type {
		case "ping":
			resp := map[string]any{"type": "pong", "ts": time.Now().UnixMilli()}
			b, _ := json.Marshal(resp)
			sendCh <- b
		case "send":
			resp := map[string]any{"type": "broadcast", "content": msg.Content}
			b, _ := json.Marshal(resp)
			sendCh <- b
		}
	}
}
