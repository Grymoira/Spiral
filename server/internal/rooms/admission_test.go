package rooms

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPublicCreateConcurrentSourceQuota(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	var wg sync.WaitGroup
	results := make(chan error, 80)
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateForSource("198.51.100.10")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, throttled := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrRateLimited):
			throttled++
		default:
			t.Fatalf("unexpected admission error: %v", err)
		}
	}
	if success != maxCreationsPerSource || throttled != 80-maxCreationsPerSource {
		t.Fatalf("source rate bound violated: success=%d throttled=%d", success, throttled)
	}
	if len(s.rooms) != success || len(s.recentCreates) != success {
		t.Fatal("rejected creations leaked rooms or rate records")
	}
}

func TestPublicCreateRateAndSourceLifetime(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	const ip = "203.0.113.1"
	created := make([]Credentials, 0, maxActiveRoomsPerSource)
	for minute := 0; minute < maxActiveRoomsPerSource/maxCreationsPerSource; minute++ {
		for i := 0; i < maxCreationsPerSource; i++ {
			c, err := s.CreateForSource(ip)
			if err != nil {
				t.Fatalf("minute %d create %d: %v", minute, i, err)
			}
			created = append(created, c)
		}
		if _, err := s.CreateForSource(ip); !errors.Is(err, ErrRateLimited) {
			t.Fatalf("expected source rate rejection, got %v", err)
		}
		now = now.Add(creationWindow)
	}
	if _, err := s.CreateForSource(ip); !errors.Is(err, ErrSourceQuota) {
		t.Fatalf("single source occupied more than active room quota: %v", err)
	}
	if _, err := s.CreateForSource("203.0.113.2"); err != nil {
		t.Fatalf("other source blocked by source quota: %v", err)
	}
	if err := s.Leave(created[0].RoomID, created[0].SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateForSource(ip); err != nil {
		t.Fatalf("host leave failed to free source quota: %v", err)
	}
}

func TestPublicCreateGlobalRateAndRecovery(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	for i := 0; i < maxCreationsPerWindow; i++ {
		if _, err := s.CreateForSource(fmt.Sprintf("198.51.100.%d", i+1)); err != nil {
			t.Fatalf("creation %d: %v", i, err)
		}
	}
	if _, err := s.CreateForSource("203.0.113.44"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("global creation rate limit missing: %v", err)
	}
	if len(s.recentCreates) != maxCreationsPerWindow {
		t.Fatal("rolling rate tracking exceeded declared budget")
	}
	now = now.Add(creationWindow)
	if _, err := s.CreateForSource("203.0.113.44"); err != nil {
		t.Fatalf("rolling window did not recover: %v", err)
	}
	if len(s.recentCreates) != 1 {
		t.Fatalf("expired rate entries not reaped: %d", len(s.recentCreates))
	}
}

func TestUnclaimedRoomsExpireAndClaimedRoomsSurvive(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	unclaimed, err := s.CreateForSource("192.0.2.1")
	if err != nil { t.Fatal(err) }
	claimedByAuth, err := s.CreateForSource("192.0.2.2")
	if err != nil { t.Fatal(err) }
	claimedByJoin, err := s.CreateForSource("192.0.2.3")
	if err != nil { t.Fatal(err) }
	if _, err := s.Authorize(claimedByAuth.RoomID, claimedByAuth.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Join(claimedByJoin.RoomID, claimedByJoin.InviteCode); err != nil {
		t.Fatal(err)
	}
	now = now.Add(unclaimedRoomLifetime)
	if _, err := s.Snapshot(unclaimed.RoomID, unclaimed.SessionToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unclaimed room not reaped: %v", err)
	}
	for _, c := range []Credentials{claimedByAuth, claimedByJoin} {
		if _, err := s.Snapshot(c.RoomID, c.SessionToken); err != nil {
			t.Fatalf("active room prematurely reaped: %v", err)
		}
	}
	if _, err := s.CreateForSource("192.0.2.1"); err != nil {
		t.Fatalf("expired source room should free its quota: %v", err)
	}
}

func TestSourceAddressCanonicalizationAndRejection(t *testing.T) {
	s := NewStore()
	c, err := s.CreateForSource("::ffff:192.0.2.8")
	if err != nil { t.Fatal(err) }
	if got := s.rooms[c.RoomID].source; got != "192.0.2.8" {
		t.Fatalf("IPv4-mapped IPv6 was not collapsed: %q", got)
	}
	if _, err := s.CreateForSource("192.0.2.8"); err != nil {
		t.Fatalf("canonical address unexpectedly rejected: %v", err)
	}
	for _, bad := range []string{"", "not-a-peer", "127.0.0.1:1234"} {
		if _, err := s.CreateForSource(bad); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("invalid source %q accepted: %v", bad, err)
		}
	}
}
