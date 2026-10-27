package realtime_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func TestControlIdempotencyAndRevisionConflictsOverWebSocket(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", "{}")
	addr := strings.TrimPrefix(server.URL, "http://")
	a := connect(t, addr)
	a.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	a.until(t, "auth_ok", nil)
	initial := a.until(t, "snapshot", nil)
	rev := uint64(state(initial)["stateRevision"].(float64))
	hash := strings.Repeat("e", 64)
	control := map[string]any{"type": "track", "sha256": hash, "durationMs": 30000,
		"operationId": "ws-track-one", "expectedRevision": rev}
	a.send(t, control)
	a.until(t, "snapshot", func(x map[string]any) bool { return playback(x)["trackId"] == hash })
	ack := a.until(t, "command_ack", nil)
	if ack["operationId"] != "ws-track-one" || ack["applied"] != true {
		t.Fatalf("missing command ack: %+v", ack)
	}
	track, _ := store.Snapshot(host.RoomID, host.SessionToken)
	ready, err := store.SetReady(host.RoomID, host.SessionToken, hash, 1, true)
	if err != nil {
		t.Fatal(err)
	}

	// A fresh operation that repeats the same hash/duration is a no-op, even
	// after Ready. The prior ID also replays without resetting Ready.
	a.send(t, control)
	retryAck := a.until(t, "command_ack", nil)
	if retryAck["operationId"] != "ws-track-one" || retryAck["applied"] != false {
		t.Fatalf("replay unexpectedly applied: %+v", retryAck)
	}
	noOp := map[string]any{"type": "track", "sha256": hash, "durationMs": 30000,
		"operationId": "ws-track-two", "expectedRevision": ready.StateRevision}
	a.send(t, noOp)
	noOpAck := a.until(t, "command_ack", nil)
	if noOpAck["applied"] != false {
		t.Fatalf("same track changed readiness: %+v", noOpAck)
	}
	current, _ := store.Snapshot(host.RoomID, host.SessionToken)
	if current.StateRevision != ready.StateRevision || !current.Members[0].Ready || current.Playback.TrackGeneration != track.Playback.TrackGeneration {
		t.Fatalf("Ready changed from repeated track: %+v", current)
	}

	// First scheduling creates one timer; retransmitting the same ID does not
	// replace it or allocate a second event.
	schedule := map[string]any{"type": "schedule", "action": "seek", "mediaMs": 0,
		"operationId": "ws-seek-once", "expectedRevision": current.StateRevision}
	a.send(t, schedule)
	a.until(t, "snapshot", func(x map[string]any) bool { return playback(x)["pending"] != nil })
	a.until(t, "command_ack", nil)
	before, _ := store.Snapshot(host.RoomID, host.SessionToken)
	event := before.Playback.Pending
	a.send(t, schedule)
	again := a.until(t, "command_ack", nil)
	if again["applied"] != false {
		t.Fatalf("retry scheduled again: %+v", again)
	}
	after, _ := store.Snapshot(host.RoomID, host.SessionToken)
	if after.StateRevision != before.StateRevision || after.Playback.Pending == nil ||
		after.Playback.Pending.ID != event.ID || after.Playback.Pending.ExecuteServerMs != event.ExecuteServerMs {
		t.Fatalf("duplicate schedule mutated event: %+v", after)
	}

	a.send(t, map[string]any{"type": "schedule", "action": "pause", "mediaMs": 0,
		"operationId": "ws-stale-one", "expectedRevision": current.StateRevision})
	if failure := a.until(t, "error", nil); failure["code"] != "revision_conflict" {
		t.Fatalf("stale revision accepted: %+v", failure)
	}
	a.send(t, map[string]any{"type": "schedule", "action": "pause", "mediaMs": 0,
		"operationId": "ws-seek-once", "expectedRevision": current.StateRevision})
	if failure := a.until(t, "error", nil); failure["code"] != "operation_conflict" {
		t.Fatalf("ID collision accepted: %+v", failure)
	}

	// Reconnect with the same session token. The last accepted command must
	// remain deduplicated across replacement of the WebSocket.
	b := connect(t, addr)
	b.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	b.until(t, "auth_ok", nil)
	b.until(t, "snapshot", nil)
	b.send(t, schedule)
	if ack := b.until(t, "command_ack", nil); ack["applied"] != false {
		t.Fatalf("reconnect replay created second event: %+v", ack)
	}
}

// Raw legacy control frames must fail rather than silently bypass revision
// checking or replay safety.
func TestLegacyControlWithoutOperationKeyRejected(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", "{}")
	ws := connect(t, strings.TrimPrefix(server.URL, "http://"))
	ws.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	ws.until(t, "auth_ok", nil)
	ws.until(t, "snapshot", nil)
	hash := strings.Repeat("e", 64)
	ws.sendRaw(t, []byte(`{"type":"track","sha256":"`+hash+`","durationMs":30000}`))
	if msg := ws.until(t, "error", nil); msg["code"] != "invalid_message" {
		t.Fatalf("legacy track accepted: %+v", msg)
	}
	current, _ := store.Snapshot(host.RoomID, host.SessionToken)
	if current.Playback.TrackID != "" {
		t.Fatal("legacy track changed state")
	}
}
