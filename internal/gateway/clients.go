package gateway

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents an authenticated WebSocket connection.
type Client struct {
	UserID string
	Conn   *websocket.Conn
	Send   chan []byte
	// last heartbeat timestamp (unix ms), use atomic for thread safety
	lastSeen atomic.Int64
}

var (
	onlineUsers   = make(map[string]*Client)
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

// touch updates the last heartbeat timestamp atomically.
func (c *Client) touch() {
	c.lastSeen.Store(time.Now().UnixMilli())
}

// GetOnlineUsers returns all currently online user IDs.
func GetOnlineUsers() []string {
	onlineUsersMu.RLock()
	defer onlineUsersMu.RUnlock()
	ids := make([]string, 0, len(onlineUsers))
	for id := range onlineUsers {
		ids = append(ids, id)
	}
	return ids
}
