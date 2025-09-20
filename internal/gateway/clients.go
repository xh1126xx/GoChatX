package gateway

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client 代表一个 websocket 连接（已认证 user）
type Client struct {
	UserID string
	Conn   *websocket.Conn
	Send   chan []byte
	// last heartbeat timestamp (unix ms)
	lastSeen int64
}

var (
	onlineUsers   = make(map[string]*Client) // userID -> client
	onlineUsersMu sync.RWMutex
)

func registerOnline(userID string, c *Client) {
	onlineUsersMu.Lock()
	onlineUsers[userID] = c
	onlineUsersMu.Unlock()
}

func unregisterOnline(userID string) {
	onlineUsersMu.Lock()
	delete(onlineUsers, userID)
	onlineUsersMu.Unlock()
}

func getOnlineClient(userID string) *Client {
	onlineUsersMu.RLock()
	c := onlineUsers[userID]
	onlineUsersMu.RUnlock()
	return c
}

// 更新心跳
func (c *Client) touch() {
	c.lastSeen = time.Now().UnixMilli()
}
