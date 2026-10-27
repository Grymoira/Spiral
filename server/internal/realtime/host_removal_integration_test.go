package realtime_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

// Exercise the public authenticated HTTP recovery path with two real WebSocket
// transports, not only in-memory Store calls. A server-side removal must
// revoke an old pending COMMIT and cannot forge the host's cancellation ACK.
func TestHostRemovalRevokesGuestAndCommittedDeadlineOverTransport(t *testing.T) {
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

	hash := strings.Repeat("a", 64)
	h.send(t, map[string]any{"type": "track", "sha256": hash, "durationMs": 30000})
	h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	g.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	for _, c := range []*testConn{h, g} {
		c.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": true})
	}
	allReady := func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true &&
			members[1].(map[string]any)["ready"] == true
	}
	h.until(t, "snapshot", allReady)
	g.until(t, "snapshot", allReady)
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	armed := h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming"
	})
	g.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming"
	})
	event := playback(armed)["pending"].(map[string]any)
	for _, c := range []*testConn{h, g} {
		c.send(t, map[string]any{"type": "arm_ack", "trackId": hash,
			"trackGeneration": 1, "eventId": event["id"]})
	}
	committed := h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "committed"
	})
	g.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "committed"
	})
	if playback(committed)["pending"].(map[string]any)["executeServerMs"] == float64(0) {
		t.Fatal("expected executable COMMIT before revocation")
	}

	req, err := http.NewRequest(http.MethodDelete,
		server.URL+"/v1/rooms/"+host.RoomID+"/members/"+guest.MemberID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+host.SessionToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("host removal HTTP status: %d", res.StatusCode)
	}
	stopped := h.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil &&
			playback(m)["playState"] == "paused" &&
			len(state(m)["members"].([]any)) == 1 &&
			state(m)["cancellation"] != nil
	})
	if state(stopped)["members"].([]any)[0].(map[string]any)["id"] != host.MemberID {
		t.Fatal("revoked member survived authoritative snapshot")
	}
	if _, committedAgain := store.CommitEvent(host.RoomID, uint64(event["id"].(float64))); committedAgain {
		t.Fatal("retired COMMIT can still fire")
	}
	oldReq, err := http.NewRequest(http.MethodGet, server.URL+"/v1/rooms/"+host.RoomID, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldReq.Header.Set("Authorization", "Bearer "+guest.SessionToken)
	oldRes, err := server.Client().Do(oldReq)
	if err != nil {
		t.Fatal(err)
	}
	oldRes.Body.Close()
	if oldRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("removed token still authorized: %d", oldRes.StatusCode)
	}
	// The surviving host cannot skip its OWN receipt after removing the guest.
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	if m := h.until(t, "error", nil); m["code"] != "cancellation_pending" {
		t.Fatalf("removal bypassed surviving host's receipt: %+v", m)
	}
	notice := state(stopped)["cancellation"].(map[string]any)
	h.send(t, map[string]any{"type": "cancel_ack", "roomEpoch": state(stopped)["roomEpoch"],
		"trackGeneration": notice["trackGeneration"], "eventId": notice["eventId"],
		"stateRevision": notice["stateRevision"]})
	h.until(t, "cancel_ack_ok", nil)
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "arming" &&
			p.(map[string]any)["executeServerMs"] == float64(0)
	})
}
