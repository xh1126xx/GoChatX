package gateway

import (
	"sync"
)

type Room struct {
	ID      string
	Clients map[string]*Client
	mu      sync.RWMutex
}

func newRoom(id string) *Room {
	return &Room{
		ID:      id,
		Clients: make(map[string]*Client),
	}
}

func (r *Room) addClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Clients[c.UserID] = c
}

func (r *Room) removeClient(c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.Clients, c.UserID)
}

func (r *Room) broadcast(b []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.Clients {
		select {
		case c.Send <- b:
		default:
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

func removeEmptyRoom(roomID string) {
	roomsMu.Lock()
	defer roomsMu.Unlock()
	r := rooms[roomID]
	if r != nil {
		r.mu.RLock()
		n := len(r.Clients)
		r.mu.RUnlock()
		if n == 0 {
			delete(rooms, roomID)
		}
	}
}
