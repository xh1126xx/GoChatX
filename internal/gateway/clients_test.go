package gateway

import (
	"sync"
	"testing"
)

func resetOnlineUsers() {
	onlineUsersMu.Lock()
	onlineUsers = make(map[string]*Client)
	onlineUsersMu.Unlock()
}

func TestRegisterOnline(t *testing.T) {
	resetOnlineUsers()
	c := &Client{UserID: "alice", Send: make(chan []byte, 10)}
	registerOnline("alice", c)

	got := getOnlineClient("alice")
	if got != c {
		t.Fatal("getOnlineClient should return the registered client")
	}
}

func TestUnregisterOnline(t *testing.T) {
	resetOnlineUsers()
	c := &Client{UserID: "bob", Send: make(chan []byte, 10)}
	registerOnline("bob", c)
	unregisterOnline("bob")

	got := getOnlineClient("bob")
	if got != nil {
		t.Fatal("getOnlineClient should return nil after unregister")
	}
}

func TestGetOnlineClient_NotFound(t *testing.T) {
	resetOnlineUsers()
	got := getOnlineClient("nobody")
	if got != nil {
		t.Fatal("getOnlineClient should return nil for unknown user")
	}
}

func TestGetOnlineUsers(t *testing.T) {
	resetOnlineUsers()
	c1 := &Client{UserID: "u1", Send: make(chan []byte, 10)}
	c2 := &Client{UserID: "u2", Send: make(chan []byte, 10)}
	registerOnline("u1", c1)
	registerOnline("u2", c2)

	users := GetOnlineUsers()
	if len(users) != 2 {
		t.Fatalf("expected 2 online users, got %d", len(users))
	}
	// Check both users are present
	found := make(map[string]bool)
	for _, u := range users {
		found[u] = true
	}
	if !found["u1"] || !found["u2"] {
		t.Fatalf("expected u1 and u2, got %v", users)
	}
}

func TestGetOnlineUsers_Empty(t *testing.T) {
	resetOnlineUsers()
	users := GetOnlineUsers()
	if len(users) != 0 {
		t.Fatalf("expected 0 online users, got %d", len(users))
	}
}

func TestClientTouch(t *testing.T) {
	c := &Client{UserID: "test", Send: make(chan []byte, 10)}
	before := c.lastSeen.Load()
	c.touch()
	after := c.lastSeen.Load()
	if after <= before {
		t.Fatal("touch should update lastSeen to a newer timestamp")
	}
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	resetOnlineUsers()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c := &Client{UserID: "concurrent", Send: make(chan []byte, 10)}
			registerOnline("concurrent", c)
		}()
		go func() {
			defer wg.Done()
			unregisterOnline("concurrent")
		}()
	}
	wg.Wait()
	// Should not panic — that's the main assertion
}
