package rooms

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCreateJoinAndSnapshot(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	if host.InviteCode == "" || host.SessionToken == "" {
		t.Fatal("missing secret")
	}
	if _, err := s.Join(host.RoomID, "wrong"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong invite: %v", err)
	}
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	if guest.InviteCode != "" {
		t.Fatal("invite secret leaked in guest credentials")
	}
	if _, err := s.Snapshot(host.RoomID, "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong token: %v", err)
	}
	snap, err := s.Snapshot(host.RoomID, guest.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if snap.StateRevision != 2 || len(snap.Members) != 2 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if err := s.Leave(host.RoomID, guest.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(host.RoomID, guest.SessionToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("left token: %v", err)
	}
	if err := s.Leave(host.RoomID, host.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Snapshot(host.RoomID, host.SessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("destroyed room: %v", err)
	}
}

func TestConcurrentCapacityIsAtomic(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	const candidates = 32
	successes := make(chan Credentials, candidates)
	failures := make(chan error, candidates)
	var wg sync.WaitGroup
	for i := 0; i < candidates; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := s.Join(host.RoomID, host.InviteCode)
			if err != nil {
				failures <- err
				return
			}
			successes <- c
		}()
	}
	wg.Wait()
	close(successes)
	close(failures)
	n := 0
	for range successes {
		n++
	}
	if n != MaxMembers-1 {
		t.Fatalf("want 4 guests, got %d", n)
	}
	for err := range failures {
		if !errors.Is(err, ErrFull) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	snap, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Members) != MaxMembers {
		t.Fatalf("capacity violated: %+v", snap)
	}
}

func TestExpiredHostRevokesRoom(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	s := NewStore()
	s.now = func() time.Time { return now }
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	if _, err := s.Snapshot(host.RoomID, host.SessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired host: %v", err)
	}
}

func TestRoomLimitAndExpirationCleanup(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	s := NewStore()
	s.now = func() time.Time { return now }
	for i := 0; i < MaxRooms; i++ {
		if _, err := s.Create(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create(); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected bounded rooms, got %v", err)
	}
	now = now.Add(24 * time.Hour)
	if _, err := s.Create(); err != nil {
		t.Fatalf("expired rooms should be collected: %v", err)
	}
}

func TestExpiredGuestSeatAndCredentialRevocation(t *testing.T) {
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	s := NewStore()
	s.now = func() time.Time { return now }
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	// No session renewal is implemented in this prototype. Put only the
	// guest past expiration while keeping host valid for this focused test.
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	g := r.members[guest.SessionToken]
	g.expiresAt = now.Add(-time.Second)
	r.members[guest.SessionToken] = g
	s.mu.Unlock()
	snap, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if snap.StateRevision != 3 || len(snap.Members) != 1 {
		t.Fatalf("expired guest not reaped: %+v", snap)
	}
	if _, err = s.Snapshot(host.RoomID, guest.SessionToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired credential: %v", err)
	}
	if _, err = s.Join(host.RoomID, host.InviteCode); err != nil {
		t.Fatalf("seat not reusable: %v", err)
	}
}
