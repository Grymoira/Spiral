package realtime_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func TestMalformedMessagesCannotMutateAuthoritativeRoom(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()

	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	ws := connect(t, strings.TrimPrefix(server.URL, "http://"))
	ws.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	ws.until(t, "auth_ok", nil)
	ws.until(t, "snapshot", nil)

	hash := strings.Repeat("a", 64)
	ws.send(t, map[string]any{"type": "track", "sha256": hash, "durationMs": 12000})
	ws.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	ws.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": true})
	ws.until(t, "snapshot", func(m map[string]any) bool {
		return state(m)["members"].([]any)[0].(map[string]any)["ready"] == true
	})

	before, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	malformed := []string{
		`{"type":"schedule","action":"seek"}`,
		`{"type":"schedule","action":"seek","mediaMs":null}`,
		`{"type":"schedule","action":"pause"}`,
		`{"type":"ready","trackId":"` + hash + `","trackGeneration":1}`,
		`{"type":"ready","trackId":"` + hash + `","trackGeneration":1,"ready":null}`,
		`{"type":"track","sha256":"` + hash + `"}`,
	}
	for _, command := range malformed {
		ws.sendRaw(t, []byte(command))
		got := ws.until(t, "error", nil)
		if got["code"] != "invalid_message" {
			t.Fatalf("expected invalid_message for %s, got %v", command, got)
		}
		current, err := store.Snapshot(host.RoomID, host.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		if current.StateRevision != before.StateRevision ||
			current.Playback.TrackGeneration != before.Playback.TrackGeneration ||
			current.Playback.Pending != nil || !current.Members[0].Ready {
			t.Fatalf("malformed command mutated authoritative room: %+v", current)
		}
	}

	// An explicit zero is a legitimate seek position, not an omitted field.
	ws.send(t, map[string]any{"type": "schedule", "action": "seek", "mediaMs": 0})
	ws.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["pending"] != nil })
	after, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if after.Playback.Pending == nil || after.Playback.Pending.Action != "seek" ||
		after.Playback.Pending.MediaMs != 0 || after.StateRevision <= before.StateRevision {
		t.Fatalf("valid zero-position seek rejected: %+v", after)
	}
	ws.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": false})
	ws.until(t, "snapshot", func(m map[string]any) bool {
		return state(m)["members"].([]any)[0].(map[string]any)["ready"] == false
	})
	after, err = store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if after.Members[0].Ready {
		t.Fatal("valid ready:false did not revoke readiness")
	}
}
