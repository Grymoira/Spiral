package realtime

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func TestWebSocketAdmissionCapsAndRelease(t *testing.T) {
	h := NewHub(rooms.NewStore())
	releases := make([]func(), 0, maxWebSocketsPerIP)
	for i := 0; i < maxWebSocketsPerIP; i++ {
		release, ok := h.acquireSocket("127.0.0.1:12345")
		if !ok {
			t.Fatalf("denied connection %d before per-IP cap", i)
		}
		releases = append(releases, release)
	}
	if _, ok := h.acquireSocket("127.0.0.1:12345"); ok {
		t.Fatal("per-IP limit bypassed")
	}
	if release, ok := h.acquireSocket("[::1]:12345"); !ok {
		t.Fatal("different address rejected")
	} else {
		release()
	}
	releases[0]()
	if release, ok := h.acquireSocket("127.0.0.1:12345"); !ok {
		t.Fatal("slot was not released")
	} else {
		release()
	}
	for _, release := range releases[1:] {
		release()
	}
	if h.activeSockets != 0 || len(h.socketsByIP) != 0 {
		t.Fatalf("capacity bookkeeping leaked sockets: %d, %+v", h.activeSockets, h.socketsByIP)
	}
}

func TestWebSocketGlobalAdmissionBound(t *testing.T) {
	h := NewHub(rooms.NewStore())
	releases := make([]func(), 0, maxOpenWebSockets)
	for i := 0; i < maxOpenWebSockets; i++ {
		release, ok := h.acquireSocket(fmt.Sprintf("127.0.%d.%d:12345", i/256, i%256))
		if !ok {
			t.Fatalf("global cap was reached prematurely at %d", i)
		}
		releases = append(releases, release)
	}
	if _, ok := h.acquireSocket("192.0.2.1:12345"); ok {
		t.Fatal("global socket limit bypassed")
	}
	for _, release := range releases {
		release()
	}
}

func TestWebSocketConcurrentAdmission(t *testing.T) {
	h := NewHub(rooms.NewStore())
	var wg sync.WaitGroup
	var mu sync.Mutex
	releases := make([]func(), 0, maxWebSocketsPerIP)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, ok := h.acquireSocket("127.0.0.1:12345"); ok {
				mu.Lock()
				releases = append(releases, release)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(releases) != maxWebSocketsPerIP || h.activeSockets != maxWebSocketsPerIP {
		t.Fatalf("race exceeded/underfilled per-IP bound: %d", len(releases))
	}
	for _, release := range releases {
		release()
	}
}

func TestSupersededPeerCannotActAsCurrentConnection(t *testing.T) {
	h := NewHub(rooms.NewStore())
	first := &peer{roomID: "room-1", memberID: "member-1"}
	second := &peer{roomID: "room-1", memberID: "member-1"}
	h.mu.Lock()
	h.clients[first.roomID] = map[string]*peer{first.memberID: first}
	h.mu.Unlock()
	if !h.isCurrent(first) {
		t.Fatal("first connection must initially be current")
	}
	h.mu.Lock()
	h.clients[first.roomID][first.memberID] = second
	h.mu.Unlock()
	if h.isCurrent(first) || !h.isCurrent(second) {
		t.Fatal("superseded connection retained authority")
	}
}
