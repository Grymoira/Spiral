package rooms

import (
	"errors"
	"testing"
)

func TestHostRemovalRecoversUnacknowledgedCancellationWithoutForgingReceipt(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	arming, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"host-recovery-play-1", start.StateRevision)
	if err != nil || !applied || arming.Playback.Pending == nil {
		t.Fatalf("initial ARM: %+v %v", arming, err)
	}
	canceled, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, false)
	if err != nil || canceled.Cancellation == nil || canceled.Playback.Pending != nil {
		t.Fatalf("expected retired ARM: %+v %v", canceled, err)
	}
	if _, err = s.SetReady(host.RoomID, guest.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}
	notice := canceled.Cancellation
	remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		canceled.RoomEpoch, notice.TrackGeneration, notice.EventID, notice.StateRevision)
	if err != nil || !accepted || remaining != 1 {
		t.Fatalf("host cancel receipt: %d %v %v", remaining, accepted, err)
	}
	if _, err = s.Schedule(host.RoomID, host.SessionToken, "play", 0); !errors.Is(err, ErrCancellationPending) {
		t.Fatalf("guest missing receipt should block playback: %v", err)
	}
	if err = s.RemoveGuest(host.RoomID, guest.SessionToken, host.MemberID); !errors.Is(err, ErrHostOnly) {
		t.Fatalf("guest could remove host: %v", err)
	}
	if err = s.RemoveGuest(host.RoomID, "", guest.MemberID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("anonymous removal accepted: %v", err)
	}
	if err = s.RemoveGuest(host.RoomID, host.SessionToken, host.MemberID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("host removed own session: %v", err)
	}
	if err = s.RemoveGuest(host.RoomID, host.SessionToken, "missing-member"); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("unknown target should not change state: %v", err)
	}
	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RemoveGuest(host.RoomID, host.SessionToken, guest.MemberID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Members) != 1 || after.Members[0].ID != host.MemberID ||
		after.StateRevision <= before.StateRevision || after.Playback.PlayState != "paused" {
		t.Fatalf("bad authority after eviction: %+v", after)
	}
	if _, err = s.Snapshot(host.RoomID, guest.SessionToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("removed token still authorized: %v", err)
	}
	if _, _, err = s.AcknowledgeCancellation(host.RoomID, guest.SessionToken,
		canceled.RoomEpoch, notice.TrackGeneration, notice.EventID, notice.StateRevision); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("removed token acknowledged cancellation: %v", err)
	}
	if err = s.RemoveGuest(host.RoomID, host.SessionToken, guest.MemberID); !errors.Is(err, ErrMemberNotFound) {
		t.Fatalf("repeat eviction should not change state: %v", err)
	}
	// Removing a departed member removes their outstanding ACK requirement;
	// it NEVER pretends the former member has produced a receipt.
	base, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	next, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"host-recovery-play-3", base.StateRevision)
	if err != nil || !applied || next.Playback.Pending == nil || next.Playback.Pending.Phase != "arming" {
		t.Fatalf("surviving host cannot start a NEW ARM: %+v %v", next, err)
	}
}

func TestHostRemovalRevokesCommittedNotYetExecutedPlayingDeadline(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	arming, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"host-recovery-play-2", start.StateRevision)
	if err != nil {
		t.Fatal(err)
	}
	event := arming.Playback.Pending
	if _, _, err = s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, event.ID); err != nil {
		t.Fatal(err)
	}
	committed, changed, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, event.ID)
	if err != nil || !changed || committed.Playback.Pending.Phase != "committed" {
		t.Fatalf("expected COMMIT: %+v %v", committed, err)
	}
	if err = s.RemoveGuest(host.RoomID, host.SessionToken, guest.MemberID); err != nil {
		t.Fatal(err)
	}
	paused, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || paused.Playback.Pending != nil || paused.Playback.PlayState != "paused" {
		t.Fatalf("eviction left executable COMMIT: %+v %v", paused, err)
	}
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok {
		t.Fatal("revoked COMMIT executed after host guest removal")
	}
}
