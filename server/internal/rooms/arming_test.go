package rooms

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func preparedRoom(t *testing.T) (*Store, Credentials, Credentials, string) {
	t.Helper()
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
	first, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 30000)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, err := s.SetReady(host.RoomID, token, hash, first.Playback.TrackGeneration, true); err != nil {
			t.Fatal(err)
		}
	}
	return s, host, guest, hash
}

func TestPlayingControlRequiresEveryMemberArmingAck(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	pending, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-start-1", start.StateRevision)
	if err != nil || !applied || pending.Playback.Pending == nil {
		t.Fatalf("schedule: %+v %v", pending, err)
	}
	event := pending.Playback.Pending
	if event.Phase != "arming" || event.ExecuteServerMs != 0 || event.ArmExpiresServerMs <= s.ServerMs() {
		t.Fatalf("an unconfirmed play had an executable deadline: %+v", event)
	}
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok {
		t.Fatal("unarmed event committed")
	}

	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 2, event.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("old generation ACK accepted: %v", err)
	}
	one, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, event.ID)
	if err != nil || committed || one.Playback.Pending.Phase != "arming" {
		t.Fatalf("one ACK committed: %+v %v", one, err)
	}
	repeat, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, event.ID)
	if err != nil || committed || repeat.StateRevision != one.StateRevision {
		t.Fatal("duplicate ACK changed revision")
	}
	if _, _, err := s.AcknowledgeArming(host.RoomID, "unauthorized", hash, 1, event.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized ACK accepted: %v", err)
	}
	before := s.ServerMs()
	all, committed, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, event.ID)
	if err != nil || !committed || all.Playback.Pending.Phase != "committed" {
		t.Fatalf("all ACKs did not commit: %+v %v", all, err)
	}
	if all.Playback.Pending.ExecuteServerMs < before+scheduleLeadMs {
		t.Fatal("COMMIT deadline was not freshly allocated")
	}
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok {
		t.Fatal("event executed before shared deadline")
	}
	if _, committed, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, event.ID); err != nil || committed {
		t.Fatal("ACK replay after commit must be harmless")
	}
	if s.PendingMatchesExact(host.RoomID, *event) {
		t.Fatal("stale arming timer still matches committed event")
	}
}

func TestUnacknowledgedPlayingControlTimesOutWithoutPlayback(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	arming, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-timeout-1", start.StateRevision)
	if err != nil || !applied {
		t.Fatalf("schedule: %v", err)
	}
	old := *arming.Playback.Pending
	if _, ok := s.ExpireArming(host.RoomID, old.ID); ok {
		t.Fatal("premature ARM expiry")
	}
	if _, _, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, old.TrackGeneration, old.ID); err != nil {
		t.Fatal(err)
	}
	// Inject an elapsed ARM deadline instead of sleeping five seconds in CI.
	s.mu.Lock()
	s.rooms[host.RoomID].playback.Pending.ArmExpiresServerMs = s.ServerMs() - 1
	s.mu.Unlock()
	canceled, expired := s.ExpireArming(host.RoomID, old.ID)
	if !expired || canceled.Playback.Pending != nil || canceled.Playback.PlayState != "paused" {
		t.Fatalf("unconfirmed ARM did not fail closed: %+v", canceled)
	}
	if _, ok := s.CommitEvent(host.RoomID, old.ID); ok {
		t.Fatal("expired event committed")
	}
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, old.TrackGeneration, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale ACK revived expired event: %v", err)
	}
}

func TestCancelOrReplaceArmingRevokesOldAcks(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	p, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-cancel-1", start.StateRevision)
	if err != nil {
		t.Fatal(err)
	}
	old := *p.Playback.Pending
	if _, _, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, old.ID); err != nil {
		t.Fatal(err)
	}
	canceled, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, false)
	if err != nil || canceled.Playback.Pending != nil {
		t.Fatalf("Ready revoked but event survived: %v", err)
	}
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale ACK accepted after cancellation: %v", err)
	}
	if _, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}
	now, _ := s.Snapshot(host.RoomID, host.SessionToken)
	if _, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"arm-replace-2", now.StateRevision); !errors.Is(err, ErrCancellationPending) || applied {
		t.Fatalf("replacement bypassed cancellation receipt barrier: %v", err)
	}
	if canceled.Cancellation == nil {
		t.Fatal("expected event-scoped canceled ARM watermark")
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, applied, err := s.AcknowledgeCancellation(host.RoomID, token, now.RoomEpoch,
			canceled.Cancellation.TrackGeneration, canceled.Cancellation.EventID,
			canceled.Cancellation.StateRevision); err != nil || !applied {
			t.Fatalf("cannot acknowledge old ARM withdrawal: %v", err)
		}
	}
	newEvent, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-replace-2", now.StateRevision)
	if err != nil || newEvent.Playback.Pending == nil || newEvent.Playback.Pending.ID == old.ID {
		t.Fatal("replacement did not change event ID after all receipts")
	}
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("old ACK accepted after event replacement: %v", err)
	}
}

func TestPlayingArmCommitUsesFreshMediaAnchor(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	r.playback.PlayState = "playing"
	r.playback.AnchorMediaMs = 2000
	r.playback.AnchorServerMs = s.ServerMs() - 3000
	r.revision++
	s.mu.Unlock()

	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	pending, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"arm-projection-1", before.StateRevision)
	if err != nil || !applied || pending.Playback.Pending == nil {
		t.Fatalf("play while already playing rejected: %v", err)
	}
	id := pending.Playback.Pending.ID
	if _, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, id); err != nil || committed {
		t.Fatalf("first ACK improperly finalized: %v", err)
	}
	result, committed, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, id)
	if err != nil || !committed {
		t.Fatalf("final ACK rejected: %v", err)
	}
	event := result.Playback.Pending
	want := mediaAt(before.Playback, event.ExecuteServerMs)
	if event.MediaMs != want {
		t.Fatalf("delayed COMMIT must reanchor media: got %d, want %d", event.MediaMs, want)
	}
}

func TestReadyLossPausesAlreadyPlayingAuthoritativeTimeline(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	r.playback.PlayState = "playing"
	r.playback.AnchorMediaMs = 1000
	r.playback.AnchorServerMs = s.ServerMs() - 800
	r.revision++
	s.mu.Unlock()
	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Playback.PlayState != "paused" || stopped.StateRevision != before.StateRevision+1 ||
		stopped.Playback.AnchorMediaMs < 1800 || stopped.Playback.AnchorMediaMs > 2200 {
		t.Fatalf("member readiness loss failed to freeze timeline: %+v", stopped.Playback)
	}
	if stopped.Playback.AnchorServerMs < before.Playback.AnchorServerMs {
		t.Fatalf("monotonic anchor went backwards: %+v", stopped.Playback)
	}
	noOp, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, false)
	if err != nil || noOp.StateRevision != stopped.StateRevision {
		t.Fatalf("repeated Ready=false bumped revision: %+v %v", noOp, err)
	}
}

func TestLateJoinAndDepartureFreezeAlreadyPlayingRoom(t *testing.T) {
	t.Run("join", func(t *testing.T) {
		s := NewStore()
		host, _ := s.Create()
		hash := strings.Repeat("d", 64)
		_, _ = s.SetTrack(host.RoomID, host.SessionToken, hash, 5000)
		_, _ = s.SetReady(host.RoomID, host.SessionToken, hash, 1, true)
		s.mu.Lock()
		s.rooms[host.RoomID].playback.PlayState = "playing"
		s.rooms[host.RoomID].playback.AnchorMediaMs = 700
		s.rooms[host.RoomID].playback.AnchorServerMs = s.ServerMs() - 500
		s.mu.Unlock()
		_, err := s.Join(host.RoomID, host.InviteCode)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := s.Snapshot(host.RoomID, host.SessionToken)
		if after.Playback.PlayState != "paused" || after.Playback.AnchorMediaMs < 1200 {
			t.Fatalf("late join did not freeze active playback: %+v", after.Playback)
		}
	})
	t.Run("leave", func(t *testing.T) {
		s, host, guest, _ := preparedRoom(t)
		s.mu.Lock()
		s.rooms[host.RoomID].playback.PlayState = "playing"
		s.rooms[host.RoomID].playback.AnchorMediaMs = 500
		s.rooms[host.RoomID].playback.AnchorServerMs = s.ServerMs() - 600
		s.mu.Unlock()
		if err := s.Leave(host.RoomID, guest.SessionToken); err != nil {
			t.Fatal(err)
		}
		after, _ := s.Snapshot(host.RoomID, host.SessionToken)
		if after.Playback.PlayState != "paused" || after.Playback.AnchorMediaMs < 1100 {
			t.Fatalf("guest departure did not freeze room: %+v", after.Playback)
		}
	})
}

func committedPlayForMemberLoss(t *testing.T, s *Store, host, guest Credentials, hash, operationID string) uint64 {
	t.Helper()
	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	arming, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, operationID, before.StateRevision)
	if err != nil || !applied || arming.Playback.Pending == nil || arming.Playback.Pending.Phase != "arming" {
		t.Fatalf("schedule: %+v applied=%v err=%v", arming, applied, err)
	}
	id := arming.Playback.Pending.ID
	if _, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash,
		arming.Playback.TrackGeneration, id); err != nil || committed {
		t.Fatalf("host ACK: committed=%v err=%v", committed, err)
	}
	confirmed, committed, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash,
		arming.Playback.TrackGeneration, id)
	if err != nil || !committed || confirmed.Playback.Pending == nil ||
		confirmed.Playback.Pending.Phase != "committed" {
		t.Fatalf("guest ACK: %+v committed=%v err=%v", confirmed, committed, err)
	}
	return id
}

func TestMemberLossFencesPreviouslyCommittedPlayingEvent(t *testing.T) {
	cases := []struct {
		name   string
		remove func(*testing.T, *Store, Credentials, Credentials)
	}{
		{"leave", func(t *testing.T, s *Store, host, guest Credentials) {
			if err := s.Leave(host.RoomID, guest.SessionToken); err != nil {
				t.Fatal(err)
			}
		}},
		{"expire", func(t *testing.T, s *Store, host, guest Credentials) {
			s.mu.Lock()
			r := s.rooms[host.RoomID]
			m := r.members[guest.SessionToken]
			m.expiresAt = s.now().Add(-time.Millisecond)
			r.members[guest.SessionToken] = m
			s.mu.Unlock()
			s.SweepExpired()
		}},
		{"join", func(t *testing.T, s *Store, host, guest Credentials) {
			if _, err := s.Join(host.RoomID, host.InviteCode); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, host, guest, hash := preparedRoom(t)
			id := committedPlayForMemberLoss(t, s, host, guest, hash, "member-loss-"+tc.name)
			tc.remove(t, s, host, guest)
			after, err := s.Snapshot(host.RoomID, host.SessionToken)
			if err != nil {
				t.Fatal(err)
			}
			if after.Playback.Pending != nil || after.Playback.PlayState != "paused" {
				t.Fatalf("membership mutation left executable playback: %+v", after.Playback)
			}
			if _, committed := s.CommitEvent(host.RoomID, id); committed {
				t.Fatal("old play committed after membership mutation")
			}
		})
	}
}

func TestForcedPauseCancelsObsoleteFuturePauseButPreservesPausedSeek(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	s.mu.Lock()
	r := s.rooms[host.RoomID]
	r.playback.PlayState = "playing"
	r.playback.AnchorMediaMs = 1000
	r.playback.AnchorServerMs = s.ServerMs() - 1000
	r.revision++
	s.mu.Unlock()
	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	future, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"forced-pause-test", before.StateRevision)
	if err != nil || future.Playback.Pending == nil {
		t.Fatalf("schedule pause: %+v %v", future, err)
	}
	old := *future.Playback.Pending
	stopped, err := s.SetReady(host.RoomID, guest.SessionToken, hash, future.Playback.TrackGeneration, false)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Playback.PlayState != "paused" || stopped.Playback.Pending != nil {
		t.Fatalf("forced pause retained future pause: %+v", stopped.Playback)
	}
	if stopped.Playback.AnchorMediaMs >= old.MediaMs {
		t.Fatalf("paused media jumped to future target: stopped=%d projected=%d",
			stopped.Playback.AnchorMediaMs, old.MediaMs)
	}
	if _, committed := s.CommitEvent(host.RoomID, old.ID); committed {
		t.Fatal("obsolete pause was committed")
	}
	if _, err := s.SetReady(host.RoomID, guest.SessionToken, hash, future.Playback.TrackGeneration, true); err != nil {
		t.Fatal(err)
	}
	current, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	seek, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 1500,
		"safe-paused-seek", current.StateRevision)
	if err != nil || !applied || seek.Playback.Pending == nil {
		t.Fatalf("schedule paused seek: %+v %v", seek, err)
	}
	preserved, err := s.SetReady(host.RoomID, guest.SessionToken, hash, future.Playback.TrackGeneration, false)
	if err != nil || preserved.Playback.Pending == nil || preserved.Playback.Pending.ID != seek.Playback.Pending.ID {
		t.Fatalf("legitimate paused seek was canceled: %+v %v", preserved.Playback, err)
	}
}

func TestCancellationReceiptRequiresExactEventRevisionAndMember(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	before, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	arming, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0,
		"cancel-receipt-test", before.StateRevision)
	if err != nil || !applied || arming.Playback.Pending == nil {
		t.Fatalf("ARM: %+v %v", arming, err)
	}
	originalID := arming.Playback.Pending.ID
	paused, err := s.SetReady(host.RoomID, guest.SessionToken, hash, arming.Playback.TrackGeneration, false)
	if err != nil || paused.Playback.Pending != nil || paused.Cancellation == nil {
		t.Fatalf("cancellation notice missing: %+v %v", paused, err)
	}
	c := paused.Cancellation
	if c.EventID != originalID || c.TrackGeneration != arming.Playback.TrackGeneration ||
		c.StateRevision != paused.StateRevision || paused.RoomEpoch == "" {
		t.Fatalf("cancellation must refer to retired event and its revision: %+v", c)
	}
	s.mu.Lock()
	original := s.rooms[host.RoomID].cancellation
	pendingInitially := len(original.outstanding)
	s.mu.Unlock()
	if pendingInitially != 2 {
		t.Fatalf("expected receipts from both live members, got %d", pendingInitially)
	}
	for _, invalid := range []struct {
		name  string
		token string
		epoch string
		gen   uint64
		id    uint64
		rev   uint64
	}{
		{"different epoch", host.SessionToken, "other-epoch", c.TrackGeneration, c.EventID, c.StateRevision},
		{"different generation", host.SessionToken, paused.RoomEpoch, c.TrackGeneration + 1, c.EventID, c.StateRevision},
		{"different event", host.SessionToken, paused.RoomEpoch, c.TrackGeneration, c.EventID + 1, c.StateRevision},
		{"different revision", host.SessionToken, paused.RoomEpoch, c.TrackGeneration, c.EventID, c.StateRevision + 1},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, invalid.token,
				invalid.epoch, invalid.gen, invalid.id, invalid.rev)
			if !errors.Is(err, ErrInvalidState) || accepted || remaining != 0 {
				t.Fatalf("forged receipt accepted: left=%d accepted=%v err=%v", remaining, accepted, err)
			}
		})
	}
	if _, accepted, err := s.AcknowledgeCancellation(host.RoomID, "invalid-session",
		paused.RoomEpoch, c.TrackGeneration, c.EventID, c.StateRevision); !errors.Is(err, ErrUnauthorized) || accepted {
		t.Fatalf("unauthorized receipt accepted: %v", err)
	}
	remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		paused.RoomEpoch, c.TrackGeneration, c.EventID, c.StateRevision)
	if err != nil || !accepted || remaining != 1 {
		t.Fatalf("host ACK: remaining=%d applied=%v err=%v", remaining, accepted, err)
	}
	remaining, accepted, err = s.AcknowledgeCancellation(host.RoomID, host.SessionToken,
		paused.RoomEpoch, c.TrackGeneration, c.EventID, c.StateRevision)
	if err != nil || accepted || remaining != 1 {
		t.Fatalf("duplicate ACK changed count: remaining=%d applied=%v err=%v", remaining, accepted, err)
	}
	remaining, accepted, err = s.AcknowledgeCancellation(host.RoomID, guest.SessionToken,
		paused.RoomEpoch, c.TrackGeneration, c.EventID, c.StateRevision)
	if err != nil || !accepted || remaining != 0 {
		t.Fatalf("guest ACK: remaining=%d applied=%v err=%v", remaining, accepted, err)
	}
	latest, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || latest.StateRevision != paused.StateRevision ||
		latest.Cancellation == nil || latest.Cancellation.EventID != c.EventID {
		t.Fatalf("ACK must not mutate authoritative playback revision: %+v %v", latest, err)
	}
}

func TestCancellationReceiptTracksOnlyLatestEventAndForgetsDepartedMember(t *testing.T) {
	s, host, guest, _ := preparedRoom(t)
	before, _ := s.Snapshot(host.RoomID, host.SessionToken)
	first, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"cancel-first-pause", before.StateRevision)
	if err != nil || !applied {
		t.Fatal(err)
	}
	second, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "seek", 1000,
		"cancel-second-seek", first.StateRevision)
	if err != nil || !applied || second.Cancellation == nil ||
		second.Cancellation.EventID != first.Playback.Pending.ID {
		t.Fatalf("replacement missed retired event: %+v %v", second, err)
	}
	firstNotice := *second.Cancellation
	third, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "pause", 0,
		"cancel-third-pause", second.StateRevision)
	if err != nil || !applied || third.Cancellation == nil ||
		third.Cancellation.EventID != second.Playback.Pending.ID {
		t.Fatalf("later replacement missed retired event: %+v %v", third, err)
	}
	if _, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken, third.RoomEpoch,
		firstNotice.TrackGeneration, firstNotice.EventID, firstNotice.StateRevision); !errors.Is(err, ErrInvalidState) || accepted {
		t.Fatalf("older cancellation should not supersede the latest: %v", err)
	}
	if err := s.Leave(host.RoomID, guest.SessionToken); err != nil {
		t.Fatal(err)
	}
	last, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || last.Cancellation == nil || last.Cancellation.EventID != third.Cancellation.EventID {
		t.Fatalf("leaving guest unexpectedly erased current tombstone: %+v %v", last, err)
	}
	remaining, accepted, err := s.AcknowledgeCancellation(host.RoomID, host.SessionToken, last.RoomEpoch,
		last.Cancellation.TrackGeneration, last.Cancellation.EventID, last.Cancellation.StateRevision)
	if err != nil || !accepted || remaining != 0 {
		t.Fatalf("departed guest left an unacknowledgeable receipt: remaining=%d accepted=%v err=%v", remaining, accepted, err)
	}
}
