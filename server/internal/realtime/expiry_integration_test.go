package realtime_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func expiryClock() (*rooms.Store, *atomic.Int64, time.Time) {
	base := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	elapsed := &atomic.Int64{}
	store := rooms.NewStoreWithClock(func() time.Time {
		return base.Add(time.Duration(elapsed.Load()))
	})
	return store, elapsed, base
}

func assertSocketClosedOnExpiry(t *testing.T, c *testConn) {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err := io.Copy(io.Discard, c.rd)
	if err == nil {
		return // EOF from the closed server connection
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("expired room socket was not revoked: %v", err)
	}
	t.Fatalf("unexpected read error waiting for session revocation: %v", err)
}

// The host and guest are given staggered test deadlines by a deliberately
// backdated guest creation clock. The shipping application does not currently
// renew host sessions, so this is a synthetic guest-first expiry scenario.
func TestGuestExpiryAfterRESTSnapshotBroadcastsCurrentMembers(t *testing.T) {
	store, elapsed, _ := expiryClock()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()

	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", "{}")
	elapsed.Store(-time.Hour.Nanoseconds())
	guest := httpRoom(t, server.Client(), server.URL+"/v1/rooms/"+host.RoomID+"/join",
		`{"inviteCode":"`+host.InviteCode+`"}`)
	elapsed.Store((22 * time.Hour).Nanoseconds())
	addr := strings.TrimPrefix(server.URL, "http://")
	h := connect(t, addr)
	g := connect(t, addr)
	h.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	g.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": guest.SessionToken})
	h.until(t, "auth_ok", nil)
	g.until(t, "auth_ok", nil)
	h.until(t, "snapshot", nil)
	g.until(t, "snapshot", nil)

	// This REST snapshot passively removes the expired guest. Other sockets
	// must be updated without requiring the host to send a control command.
	elapsed.Store((23*time.Hour + time.Minute).Nanoseconds())
	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/rooms/"+host.RoomID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+host.SessionToken)
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("host snapshot returned %d", res.StatusCode)
	}
	updated := h.until(t, "snapshot", func(msg map[string]any) bool {
		s := state(msg)
		return len(s["members"].([]any)) == 1 && s["stateRevision"].(float64) == 3
	})
	if got := len(state(updated)["members"].([]any)); got != 1 {
		t.Fatalf("expired guest still visible: %d", got)
	}
	assertSocketClosedOnExpiry(t, g)
	if _, err := store.Snapshot(host.RoomID, guest.SessionToken); !errors.Is(err, rooms.ErrUnauthorized) {
		t.Fatalf("expired guest token accepted: %v", err)
	}
	// No repeated expiration event may bump the revision a second time.
	next, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || next.StateRevision != 3 {
		t.Fatalf("duplicate sweep changed revision: %+v %v", next, err)
	}
}

func TestProactiveHostExpiryClosesAllRoomSockets(t *testing.T) {
	store, elapsed, _ := expiryClock()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", "{}")
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

	// No REST or application WS command is made after advancing beyond
	// expiration: the single active Hub sweeper must detect the dead room.
	elapsed.Store((24*time.Hour + time.Second).Nanoseconds())
	assertSocketClosedOnExpiry(t, h)
	assertSocketClosedOnExpiry(t, g)
	if _, err := store.Snapshot(host.RoomID, host.SessionToken); !errors.Is(err, rooms.ErrNotFound) {
		t.Fatalf("expired host retained room: %v", err)
	}
}
