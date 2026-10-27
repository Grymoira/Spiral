package rooms

import (
	"errors"
	"strings"
	"testing"
)

func TestPlayingBlockedUntilEveryCancellationReceiptArrives(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	current, _ := s.Snapshot(host.RoomID, host.SessionToken)
	arming, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"barrier-original-play", current.StateRevision)
	if err != nil || !applied || arming.Playback.Pending == nil {
		t.Fatalf("prepare play: %+v applied=%v err=%v", arming, applied, err)
	}
	canceled, err := s.SetReady(host.RoomID, guest.SessionToken, hash, arming.Playback.TrackGeneration, false)
	if err != nil || canceled.Playback.Pending != nil || canceled.Cancellation == nil {
		t.Fatalf("cancel play: %+v err=%v", canceled, err)
	}
	notice := *canceled.Cancellation
	ready, err := s.SetReady(host.RoomID, guest.SessionToken, hash, arming.Playback.TrackGeneration, true)
	if err != nil {
		t.Fatal(err)
	}
	assertBlocked := func() {
		t.Helper()
		if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
			"barrier-new-play", ready.StateRevision); !errors.Is(err, ErrCancellationPending) || applied {
			t.Fatalf("allowed playing before everyone confirmed cancellation: applied=%v err=%v", applied, err)
		}
		if _, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0); !errors.Is(err, ErrCancellationPending) {
			t.Fatalf("legacy schedule bypassed cancellation barrier: %v", err)
		}
		check, err := s.Snapshot(host.RoomID, host.SessionToken)
		if err != nil || check.StateRevision != ready.StateRevision || check.Playback.Pending != nil {
			t.Fatalf("rejected play changed room: %+v err=%v", check, err)
		}
	}
	assertBlocked()
	remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		ready.RoomEpoch, notice.TrackGeneration, notice.EventID, notice.StateRevision)
	if err != nil || !accepted || remaining != 1 {
		t.Fatalf("host receipt: remaining=%d applied=%v err=%v", remaining, accepted, err)
	}
	assertBlocked()
	remaining, accepted, err = s.AcknowledgeCancellation(host.RoomID, guest.SessionToken,
		ready.RoomEpoch, notice.TrackGeneration, notice.EventID, notice.StateRevision)
	if err != nil || !accepted || remaining != 0 {
		t.Fatalf("guest receipt: remaining=%d applied=%v err=%v", remaining, accepted, err)
	}
	allowed, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"barrier-new-play", ready.StateRevision)
	if err != nil || !applied || allowed.Playback.Pending == nil ||
		allowed.Playback.Pending.Phase != "arming" {
		t.Fatalf("all receipts should unlock ARM: %+v applied=%v err=%v", allowed, applied, err)
	}
}

func TestLatestCancellationReceiptIsCumulativeAcrossRapidReplacements(t *testing.T) {
	s, host, guest, _ := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	first, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"watermark-first", start.StateRevision)
	if err != nil || !applied {
		t.Fatal(err)
	}
	second, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 1000,
		"watermark-second", first.StateRevision)
	if err != nil || !applied || second.Cancellation == nil {
		t.Fatalf("first cancellation missing: %+v %v", second, err)
	}
	old := *second.Cancellation
	// The host already acknowledged the first of two rapid cancellations.
	if remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		second.RoomEpoch, old.TrackGeneration, old.EventID, old.StateRevision); err != nil || !accepted || remaining != 1 {
		t.Fatalf("old host receipt: %d %v %v", remaining, accepted, err)
	}
	third, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"watermark-third", second.StateRevision)
	if err != nil || !applied || third.Cancellation == nil ||
		third.Cancellation.EventID != second.Playback.Pending.ID {
		t.Fatalf("second cancellation missing: %+v %v", third, err)
	}
	latest := *third.Cancellation
	if _, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		third.RoomEpoch, old.TrackGeneration, old.EventID, old.StateRevision); !errors.Is(err, ErrInvalidState) || accepted {
		t.Fatalf("stale receipt cleared latest watermark: %v", err)
	}
	if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"watermark-playing", third.StateRevision); !errors.Is(err, ErrCancellationPending) || applied {
		t.Fatalf("replacing another pending event with playing did not fail closed: %v", err)
	}
	// A future audible event is still denied even after the safe appointment
	// commits, because both members must acknowledge the cumulative watermark.
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	r.playback.Pending = nil
	r.revision++
	latestRevision := r.revision
	s.mu.Unlock()
	if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"watermark-playing", latestRevision); !errors.Is(err, ErrCancellationPending) || applied {
		t.Fatalf("unconfirmed watermark bypassed without pending: %v", err)
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, accepted, err := s.AcknowledgeCancellation(host.RoomID, token,
			third.RoomEpoch, latest.TrackGeneration, latest.EventID, latest.StateRevision); err != nil || !accepted {
			t.Fatalf("current cumulative receipt rejected: %v", err)
		}
	}
	ready, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"watermark-playing", latestRevision)
	if err != nil || !applied || ready.Playback.Pending == nil {
		t.Fatalf("latest receipts did not release barrier: %+v %v", ready, err)
	}
}

func TestReadyLossWithCanceledEventStillRequiresReceiptAfterReadinessRestored(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	before, _ := s.Snapshot(host.RoomID, host.SessionToken)
	arming, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"receipt-readiness-loss", before.StateRevision)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := s.SetReady(host.RoomID, guest.SessionToken, hash, arming.Playback.TrackGeneration, false)
	if err != nil || stop.Cancellation == nil {
		t.Fatalf("ready loss did not create notice: %+v %v", stop, err)
	}
	s.mu.Lock()
	if len(s.rooms[host.RoomID].cancellation.outstanding) != 2 {
		s.mu.Unlock()
		t.Fatal("expected outstanding receipts from both members")
	}
	s.mu.Unlock()
	if _, err := s.SetReady(host.RoomID, guest.SessionToken, hash, arming.Playback.TrackGeneration, true); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Snapshot(host.RoomID, host.SessionToken)
	if _, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0); !errors.Is(err, ErrCancellationPending) {
		t.Fatalf("restored Ready bypassed receipt gate: %v", err)
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		_, accepted, err := s.AcknowledgeCancellation(host.RoomID, token,
			current.RoomEpoch, stop.Cancellation.TrackGeneration,
			stop.Cancellation.EventID, stop.Cancellation.StateRevision)
		if err != nil || !accepted {
			t.Fatalf("receipt rejected: %v", err)
		}
	}
	if next, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0); err != nil || next.Playback.Pending == nil {
		t.Fatalf("play remained blocked after all receipts: %+v %v", next, err)
	}
}

func TestDeadlineFailsClosedIfCancellationPendingDespiteCorruptedPendingPlayingState(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("e", 64)
	if _, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 20000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetReady(host.RoomID, host.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Snapshot(host.RoomID, host.SessionToken)
	first, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"deadline-safe-first", current.StateRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 200,
		"deadline-safe-second", first.StateRevision); err != nil {
		t.Fatal(err)
	}
	// Simulate an adversarial stale callback exposing an executable pending
	// play despite outstanding canceled-event receipts. Public API refuses
	// to create such a state, so inject it only for this invariant test.
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	r.nextEvent++
	id := r.nextEvent
	r.playback.Pending = &ScheduledEvent{ID: id, Action: "play", TrackGeneration: 1,
		Phase: "committed", PlayState: "playing", ExecuteServerMs: s.ServerMs() - 1}
	r.revision++
	s.mu.Unlock()
	state, applied := s.CommitEvent(host.RoomID, id)
	if applied || state.Playback.PlayState != "paused" || state.Playback.Pending != nil ||
		state.Cancellation == nil || state.Cancellation.EventID != id {
		t.Fatalf("stale pending executable event escaped final fence: %+v applied=%v", state, applied)
	}
}
