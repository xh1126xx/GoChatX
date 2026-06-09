package gateway

import (
	"log/slog"
	"sync"
)

// Room represents a chat room with a set of clients.
type Room struct {
	ID      string
	clients map[string]*Client
	mu      sync.RWMutex
}

func newRoom(id string) *Room {
	return &Room{
		ID:      id,
		clients: make(map[string]*Client),
	}
}

func (r *Room) addClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[c.UserID] = c
}

func (r *Room) removeClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, c.UserID)
}

// Contains reports whether the room has the given user.
func (r *Room) Contains(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.clients[userID]
	return ok
}

func (r *Room) broadcast(b []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		select {
		case c.Send <- b:
		default:
			slog.Warn("broadcast drop (channel full)", "user_id", c.UserID)
		}
	}
}

var (
	rooms   = make(map[string]*Room)
	roomsMu sync.RWMutex
)

func getOrCreateRoom(roomID string) *Room {
	roomsMu.RLock()
	r := rooms[roomID]
	roomsMu.RUnlock()
	if r != nil {
		return r
	}
	roomsMu.Lock()
	defer roomsMu.Unlock()
	r = rooms[roomID]
	if r == nil {
		r = newRoom(roomID)
		rooms[roomID] = r
	}
	return r
}

// removeEmptyRoom atomically checks and removes an empty room under write lock.
func removeEmptyRoom(roomID string) {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	r := rooms[roomID]
	if r != nil {
		r.mu.Lock()
		n := len(r.clients)
		r.mu.Unlock()
		if n == 0 {
			delete(rooms, roomID)
		}
	}
}
