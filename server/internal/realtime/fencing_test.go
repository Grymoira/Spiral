package realtime

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

// Test peers use real closable connections, but no writer goroutines.
func fencingTestPeer(t *testing.T, credentials rooms.Credentials) *peer {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return &peer{
		conn: &socket{Conn: server}, roomID: credentials.RoomID,
		memberID: credentials.MemberID, token: credentials.SessionToken,
		outgoing: make(chan []byte, 32), shutdown: make(chan struct{}),
	}
}

func TestSupersededConnectionCannotMutateRoom(t *testing.T) {
	store := rooms.NewStore()
	host, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(store)
	old := fencingTestPeer(t, host)
	current := fencingTestPeer(t, host)
	h.register(old)
	h.register(current)
	defer h.unregister(current)

	hash := strings.Repeat("a", 64)
	if _, err := store.SetTrack(host.RoomID, host.SessionToken, hash, 10000); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetReady(host.RoomID, host.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}

	commands := []envelope{
		{Type: "track", SHA256: strings.Repeat("b", 64), DurationMs: 10000, OperationID: "fence-track", ExpectedRevision: before.StateRevision},
		{Type: "ready", TrackID: hash, TrackGeneration: 1, Ready: false},
		{Type: "schedule", Action: "pause", MediaMs: 0, OperationID: "fence-pause", ExpectedRevision: before.StateRevision},
	}
	for _, cmd := range commands {
		if _, _, err := h.applyCommand(old, cmd); !errors.Is(err, errSuperseded) {
			t.Fatalf("superseded %s command: expected fencing, got %v", cmd.Type, err)
		}
		after, err := store.Snapshot(host.RoomID, host.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		if after.StateRevision != before.StateRevision ||
			after.Playback.TrackGeneration != before.Playback.TrackGeneration ||
			after.Playback.Pending != nil || !after.Members[0].Ready {
			t.Fatalf("superseded %s command changed authoritative state", cmd.Type)
		}
	}
	// The replacement must retain authority.
	if _, _, err := h.applyCommand(current, envelope{Type: "schedule", Action: "pause", MediaMs: 0, OperationID: "current-pause", ExpectedRevision: before.StateRevision}); err != nil {
		t.Fatalf("replacement lost command authority: %v", err)
	}
}

func TestSupersededUnregisterPreservesNewConnectionReadiness(t *testing.T) {
	store := rooms.NewStore()
	host, err := store.Create()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(store)
	old := fencingTestPeer(t, host)
	current := fencingTestPeer(t, host)
	h.register(old)
	hash := strings.Repeat("c", 64)
	if _, err := store.SetTrack(host.RoomID, host.SessionToken, hash, 10000); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetReady(host.RoomID, host.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}

	h.register(current)
	// Every connection replacement starts unready.
	snap, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Members[0].Ready {
		t.Fatal("replacement did not clear stale readiness")
	}
	if _, _, err := h.applyCommand(current, envelope{Type: "ready", TrackID: hash, TrackGeneration: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
	h.unregister(old)
	snap, err = store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Members[0].Ready {
		t.Fatal("superseded unregister revoked replacement's readiness")
	}
	h.unregister(current)
	snap, err = store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Members[0].Ready {
		t.Fatal("current unregister failed to revoke readiness")
	}
}
