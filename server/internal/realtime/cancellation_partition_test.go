package realtime_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

// Deliberately withhold one cancellation receipt while the TCP socket stays
// open and that member has restored Ready. This simulates a lost outbound ACK,
// and proves that the room cannot publish another playing deadline merely
// because the ordinary Ready barrier is satisfied.
func TestMissingCancellationAckBlocksPlayingOverRealTwoSocketTransport(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	guest := httpRoom(t, server.Client(), server.URL+"/v1/rooms/"+host.RoomID+"/join",
		`{"inviteCode":"`+host.InviteCode+`"}`)
	addr := strings.TrimPrefix(server.URL, "http://")
	h := connect(t, addr)
	g := connect(t, addr)
	h.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	g.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": guest.SessionToken})
	h.until(t, "auth_ok", nil)
	g.until(t, "auth_ok", nil)
	h.until(t, "snapshot", nil)
	g.until(t, "snapshot", nil)
	hash := strings.Repeat("f", 64)
	h.send(t, map[string]any{"type": "track", "sha256": hash, "durationMs": 20000})
	h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	g.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	for _, c := range []*testConn{h, g} {
		c.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": true})
	}
	h.until(t, "snapshot", func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true &&
			members[1].(map[string]any)["ready"] == true
	})
	g.until(t, "snapshot", func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true &&
			members[1].(map[string]any)["ready"] == true
	})
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming"
	})
	g.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming"
	})
	// A participant withdraws Ready. The old ARM is retired and its
	// cancellation is sent to both sockets.
	g.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": false})
	retiredH := h.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil && state(m)["cancellation"] != nil
	})
	retiredG := g.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil && state(m)["cancellation"] != nil
	})
	if state(retiredH)["cancellation"].(map[string]any)["eventId"] !=
		state(retiredG)["cancellation"].(map[string]any)["eventId"] {
		t.Fatal("different cancellation event sent to host and guest")
	}
	// The guest restores ordinary Ready but deliberately withholds its ACK.
	g.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": true})
	readyH := h.until(t, "snapshot", func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true &&
			members[1].(map[string]any)["ready"] == true
	})
	g.until(t, "snapshot", func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true &&
			members[1].(map[string]any)["ready"] == true
	})
	notice := state(readyH)["cancellation"].(map[string]any)
	ack := map[string]any{
		"type":            "cancel_ack",
		"roomEpoch":       state(readyH)["roomEpoch"],
		"trackGeneration": notice["trackGeneration"],
		"eventId":         notice["eventId"],
		"stateRevision":   notice["stateRevision"],
	}
	h.send(t, ack)
	h.until(t, "cancel_ack_ok", nil)
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	if m := h.until(t, "error", nil); m["code"] != "cancellation_pending" {
		t.Fatalf("guest missing ACK failed to block new play: %+v", m)
	}
	// A recovered guest acknowledges the same cumulative state. Only now
	// can a new ARM be broadcast; no playing deadline is published early.
	g.send(t, ack)
	g.until(t, "cancel_ack_ok", nil)
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	arming := h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming"
	})
	if playback(arming)["pending"].(map[string]any)["executeServerMs"] != float64(0) {
		t.Fatalf("new play unexpectedly skipped per-member ARM barrier: %+v", playback(arming))
	}
}
