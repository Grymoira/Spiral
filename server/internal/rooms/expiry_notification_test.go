package rooms

import (
	"errors"
	"testing"
	"time"
)

func TestExpirationSignalsCoalesceAndRemainNonblocking(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s := NewStoreWithClock(func() time.Time { return now })
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	g := r.members[guest.SessionToken]
	g.expiresAt = now.Add(-time.Second)
	r.members[guest.SessionToken] = g
	s.mu.Unlock()

	for i := 0; i < 100; i++ {
		s.SweepExpired()
	}
	select {
	case <-s.ExpirationNotifications():
	default:
		t.Fatal("no signal for expired guest")
	}
	select {
	case <-s.ExpirationNotifications():
		t.Fatal("repeated sweep produced duplicate expiry notifications")
	default:
	}
	snap, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || len(snap.Members) != 1 || snap.StateRevision != 3 {
		t.Fatalf("wrong expired guest state: %+v %v", snap, err)
	}
	now = now.Add(24 * time.Hour)
	s.SweepExpired()
	select {
	case <-s.ExpirationNotifications():
	default:
		t.Fatal("no signal for expired host/room")
	}
	if _, err := s.Snapshot(host.RoomID, host.SessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired host was not removed: %v", err)
	}
}

func TestInactiveUnclaimedRoomExpiryNotifiesOnce(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s := NewStoreWithClock(func() time.Time { return now })
	h, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(unclaimedRoomLifetime)
	s.SweepExpired()
	if _, err := s.Snapshot(h.RoomID, h.SessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unclaimed expired room survived: %v", err)
	}
	select {
	case <-s.ExpirationNotifications():
	default:
		t.Fatal("unclaimed expiration was not signaled")
	}
}
