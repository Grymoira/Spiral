package realtime_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

// A client recovering from suspend must be able to request a new authoritative
// snapshot on the same authenticated socket. Correlation prevents an older
// unsolicited broadcast from accidentally releasing its local recovery fence.
func TestAuthenticatedSnapshotResyncReturnsCurrentRevisionAndRequestID(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()

	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	ws := connect(t, strings.TrimPrefix(server.URL, "http://"))
	ws.send(t, map[string]any{"type": "auth", "roomId": host.RoomID,
		"sessionToken": host.SessionToken})
	ws.until(t, "auth_ok", nil)
	ws.until(t, "snapshot", nil)

	requestID := "recover-initial-001"
	ws.send(t, map[string]any{"type": "snapshot", "operationId": requestID})
	first := ws.until(t, "snapshot", func(m map[string]any) bool {
		return m["operationId"] == requestID
	})
	if state(first)["roomId"] != host.RoomID {
		t.Fatalf("unicast snapshot belonged to wrong room: %+v", first)
	}
	before, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(state(first)["stateRevision"].(float64)) != before.StateRevision {
		t.Fatalf("snapshot reply is not the store revision: got=%v want=%d",
			state(first)["stateRevision"], before.StateRevision)
	}
	if before.Playback.TrackID != "" {
		t.Fatal("fresh room has unexpected track")
	}

	hash := strings.Repeat("d", 64)
	ws.send(t, map[string]any{"type": "track", "sha256": hash, "durationMs": 18000})
	ws.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["trackId"] == hash
	})
	after, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if after.StateRevision <= before.StateRevision {
		t.Fatalf("room revision did not change after track: %+v", after)
	}
	requestID = "recover-latest-002"
	ws.send(t, map[string]any{"type": "snapshot", "operationId": requestID})
	fresh := ws.until(t, "snapshot", func(m map[string]any) bool {
		return m["operationId"] == requestID
	})
	if uint64(state(fresh)["stateRevision"].(float64)) != after.StateRevision ||
		playback(fresh)["trackId"] != hash ||
		state(fresh)["roomEpoch"] != after.RoomEpoch {
		t.Fatalf("correlated snapshot did not return latest authoritative state: %+v", fresh)
	}
	again, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || again.StateRevision != after.StateRevision {
		t.Fatalf("read-only request unexpectedly mutated room revision: %+v, %v", again, err)
	}

	// A malformed resync command is rejected before it can modify the room.
	ws.sendRaw(t, []byte(`{"type":"snapshot","operationId":"bad id"}`))
	if result := ws.until(t, "error", nil); result["code"] != "invalid_message" {
		t.Fatalf("accepted invalid snapshot request: %+v", result)
	}
	final, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || final.StateRevision != again.StateRevision {
		t.Fatalf("invalid resync request changed room: %+v, %v", final, err)
	}
}
