package rooms

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestControlledTrackNoopAndExplicitReload(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	start, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	first, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "new-track-1", start.StateRevision, false)
	if err != nil || !applied || first.Playback.TrackGeneration != 1 {
		t.Fatalf("first track: %+v, %v, %v", first, applied, err)
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, err := s.SetReady(host.RoomID, token, hash, 1, true); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	noOp, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "new-track-2", ready.StateRevision, false)
	if err != nil || applied || noOp.StateRevision != ready.StateRevision || noOp.Playback.TrackGeneration != 1 ||
		!noOp.Members[0].Ready || !noOp.Members[1].Ready {
		t.Fatalf("identical track changed readiness: %+v, applied=%v, err=%v", noOp, applied, err)
	}
	// Replay the ORIGINAL operation after other members became Ready.
	replayed, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "new-track-1", start.StateRevision, false)
	if err != nil || applied || replayed.StateRevision != ready.StateRevision {
		t.Fatalf("original command replay mutated room: %+v, %v, %v", replayed, applied, err)
	}
	reload, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "reload-track-1", noOp.StateRevision, true)
	if err != nil || !applied || reload.Playback.TrackGeneration != 2 || reload.StateRevision <= ready.StateRevision ||
		reload.Members[0].Ready || reload.Members[1].Ready {
		t.Fatalf("explicit reload failed to invalidate Ready: %+v, %v, %v", reload, applied, err)
	}
	again, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "reload-track-1", noOp.StateRevision, true)
	if err != nil || applied || again.Playback.TrackGeneration != 2 {
		t.Fatalf("reload replay applied twice: %+v %v %v", again, applied, err)
	}
}

func TestControlledScheduleIdempotencyStaleAndPayloadCollision(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 64)
	track, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 30000)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := s.SetReady(host.RoomID, host.SessionToken, hash, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	scheduled, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 0, "seek-command-1", ready.StateRevision)
	if err != nil || !applied || scheduled.Playback.Pending == nil {
		t.Fatalf("schedule failed: %+v %v %v", scheduled, applied, err)
	}
	id := scheduled.Playback.Pending.ID
	deadline := scheduled.Playback.Pending.ExecuteServerMs
	again, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 0, "seek-command-1", ready.StateRevision)
	if err != nil || applied || again.Playback.Pending == nil || again.Playback.Pending.ID != id ||
		again.Playback.Pending.ExecuteServerMs != deadline || again.StateRevision != scheduled.StateRevision {
		t.Fatalf("replayed schedule reset deadline or ID: %+v %v %v", again, applied, err)
	}
	if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 100, "seek-command-1", ready.StateRevision); !errors.Is(err, ErrOperationConflict) || applied {
		t.Fatalf("operation ID reuse with different payload: %v %v", err, applied)
	}
	if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0, "different-id-1", ready.StateRevision); !errors.Is(err, ErrRevisionConflict) || applied {
		t.Fatalf("stale revision accepted: %v %v", err, applied)
	}
	if _, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 30000, "seek-command-1", ready.StateRevision, false); !errors.Is(err, ErrOperationConflict) || applied {
		t.Fatalf("operation ID reused across kinds: %v %v", err, applied)
	}
	if scheduled.Playback.TrackGeneration != track.Playback.TrackGeneration {
		t.Fatal("track generation changed")
	}
	time.Sleep(time.Duration(deadline-s.ServerMs()+5) * time.Millisecond)
	committed, ok := s.CommitEvent(host.RoomID, id)
	if !ok {
		t.Fatal("scheduled seek did not commit")
	}
	replayed, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 0, "seek-command-1", ready.StateRevision)
	if err != nil || applied || replayed.Playback.Pending != nil || replayed.StateRevision != committed.StateRevision {
		t.Fatalf("retry after committed event recreated pending: %+v %v %v", replayed, applied, err)
	}
}

func TestControlledCommandsConcurrentAndBounded(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("c", 64)
	initial, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 12000, "concurrent-track-1", initial.StateRevision, false)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || after.Playback.TrackGeneration != 1 || after.StateRevision != initial.StateRevision+1 {
		t.Fatalf("concurrent retry modified state repeatedly: %+v %v", after, err)
	}
	// Produce more than 64 *successful* distinct operations without changing
	// revision; bound memory and ensure evicted stale ops cannot be executed.
	for i := 0; i < maxRememberedOperations+5; i++ {
		_, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 12000,
			fmt.Sprintf("noop-operation-%03d", i), after.StateRevision, false)
		if err != nil || applied {
			t.Fatalf("no-op %d: %v %v", i, applied, err)
		}
	}
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	receiptCount, orderCount := len(r.operations), len(r.operationOrder)
	s.mu.Unlock()
	if receiptCount != maxRememberedOperations || orderCount != maxRememberedOperations {
		t.Fatalf("unbounded dedup cache: %d %d", receiptCount, orderCount)
	}
	// Eviction doesn't silently turn an old operation into a new authorized
	// command if the room's authoritative revision has moved on.
	reloaded, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 12000,
		"new-reload-100", after.StateRevision, true)
	if err != nil || !applied {
		t.Fatalf("reload failure: %+v %v", reloaded, err)
	}
	if _, applied, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 12000,
		"concurrent-track-1", initial.StateRevision, true); !errors.Is(err, ErrRevisionConflict) || applied {
		t.Fatalf("evicted old intent bypassed precondition: %v %v", err, applied)
	}
}

func TestControlledNonHostForbiddenEvenWhenReusingHostKey(t *testing.T) {
	s := NewStore()
	host, _ := s.Create()
	guest, _ := s.Join(host.RoomID, host.InviteCode)
	snap, _ := s.Snapshot(host.RoomID, host.SessionToken)
	hash := strings.Repeat("d", 64)
	_, _, err := s.ControlTrack(host.RoomID, host.SessionToken, hash, 10000,
		"host-operation-1", snap.StateRevision, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ControlTrack(host.RoomID, guest.SessionToken, hash, 10000,
		"host-operation-1", snap.StateRevision, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("guest hijacked host dedup key: %v", err)
	}
}
