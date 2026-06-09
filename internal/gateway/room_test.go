package gateway

import (
	"sync"
	"testing"
)

func TestNewRoom(t *testing.T) {
	r := newRoom("test-room")
	if r.ID != "test-room" {
		t.Fatalf("expected room ID 'test-room', got '%s'", r.ID)
	}
	if len(r.clients) != 0 {
		t.Fatalf("expected empty clients map, got %d", len(r.clients))
	}
}

func TestRoomAddRemoveClient(t *testing.T) {
	r := newRoom("test")
	c := &Client{UserID: "user1", Send: make(chan []byte, 10)}

	r.addClient(c)
	if !r.Contains("user1") {
		t.Fatal("expected room to contain user1 after addClient")
	}

	r.removeClient(c)
	if r.Contains("user1") {
		t.Fatal("expected room to not contain user1 after removeClient")
	}
}

func TestRoomContains(t *testing.T) {
	r := newRoom("test")
	if r.Contains("nobody") {
		t.Fatal("empty room should not contain any user")
	}

	c := &Client{UserID: "user1", Send: make(chan []byte, 10)}
	r.addClient(c)
	if !r.Contains("user1") {
		t.Fatal("room should contain user1")
	}
	if r.Contains("user2") {
		t.Fatal("room should not contain user2")
	}
}

func TestRoomBroadcast(t *testing.T) {
	r := newRoom("test")
	ch1 := make(chan []byte, 10)
	ch2 := make(chan []byte, 10)
	c1 := &Client{UserID: "u1", Send: ch1}
	c2 := &Client{UserID: "u2", Send: ch2}

	r.addClient(c1)
	r.addClient(c2)

	msg := []byte(`{"type":"message","msg":"hello"}`)
	r.broadcast(msg)
	select {
	case got := <-ch1:
		if string(got) != string(msg) {
			t.Fatalf("client1 got wrong message: %s", got)
		}
	default:
		t.Fatal("client1 should have received a message")
	}
	select {
	case got := <-ch2:
		if string(got) != string(msg) {
			t.Fatalf("client2 got wrong message: %s", got)
		}
	default:
		t.Fatal("client2 should have received a message")
	}
}

func TestGetOrCreateRoom(t *testing.T) {
	// Clean up global state
	roomsMu.Lock()
	rooms = make(map[string]*Room)
	roomsMu.Unlock()

	r1 := getOrCreateRoom("room-a")
	if r1 == nil {
		t.Fatal("getOrCreateRoom should not return nil")
	}
	if r1.ID != "room-a" {
		t.Fatalf("expected room ID 'room-a', got '%s'", r1.ID)
	}

	// Same room should return same pointer
	r2 := getOrCreateRoom("room-a")
	if r1 != r2 {
		t.Fatal("getOrCreateRoom should return same room for same ID")
	}

	// Different room should return different pointer
	r3 := getOrCreateRoom("room-b")
	if r1 == r3 {
		t.Fatal("getOrCreateRoom should return different room for different ID")
	}
}

func TestGetOrCreateRoom_Concurrent(t *testing.T) {
	roomsMu.Lock()
	rooms = make(map[string]*Room)
	roomsMu.Unlock()

	var wg sync.WaitGroup
	results := make([]*Room, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = getOrCreateRoom("concurrent-room")
		}(i)
	}
	wg.Wait()

	// All should be the same room
	for i := 1; i < 100; i++ {
		if results[i] != results[0] {
			t.Fatalf("concurrent getOrCreateRoom returned different rooms at index %d", i)
		}
	}
}

func TestRemoveEmptyRoom(t *testing.T) {
	roomsMu.Lock()
	rooms = make(map[string]*Room)
	roomsMu.Unlock()

	r := getOrCreateRoom("empty-room")
	if r == nil {
		t.Fatal("room should exist")
	}

	removeEmptyRoom("empty-room")
	roomsMu.RLock()
	_, exists := rooms["empty-room"]
	roomsMu.RUnlock()
	if exists {
		t.Fatal("empty room should have been removed")
	}
}

func TestRemoveNonEmptyRoom(t *testing.T) {
	roomsMu.Lock()
	rooms = make(map[string]*Room)
	roomsMu.Unlock()

	r := getOrCreateRoom("busy-room")
	c := &Client{UserID: "user1", Send: make(chan []byte, 10)}
	r.addClient(c)

	removeEmptyRoom("busy-room")
	roomsMu.RLock()
	_, exists := rooms["busy-room"]
	roomsMu.RUnlock()
	if !exists {
		t.Fatal("non-empty room should NOT have been removed")
	}
}
