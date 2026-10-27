package rooms

import (
	"errors"
	"strings"
	"testing"
)

func preparedRoom(t *testing.T) (*Store, Credentials, Credentials, string) {
	t.Helper()
	s := NewStore()
	host, err := s.Create()
	if err != nil { t.Fatal(err) }
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil { t.Fatal(err) }
	hash := strings.Repeat("a", 64)
	first, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 30000)
	if err != nil { t.Fatal(err) }
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, err := s.SetReady(host.RoomID, token, hash, first.Playback.TrackGeneration, true); err != nil { t.Fatal(err) }
	}
	return s, host, guest, hash
}

func TestPlayingControlRequiresEveryMemberArmingAck(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	pending, applied, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-start-1", start.StateRevision)
	if err != nil || !applied || pending.Playback.Pending == nil { t.Fatalf("schedule: %+v %v", pending, err) }
	event := pending.Playback.Pending
	if event.Phase != "arming" || event.ExecuteServerMs != 0 || event.ArmExpiresServerMs <= s.ServerMs() {
		t.Fatalf("an unconfirmed play had an executable deadline: %+v", event)
	}
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok { t.Fatal("unarmed event committed") }

	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 2, event.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("old generation ACK accepted: %v", err)
	}
	one, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, event.ID)
	if err != nil || committed || one.Playback.Pending.Phase != "arming" { t.Fatalf("one ACK committed: %+v %v", one, err) }
	repeat, committed, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, event.ID)
	if err != nil || committed || repeat.StateRevision != one.StateRevision { t.Fatal("duplicate ACK changed revision") }
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
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok { t.Fatal("event executed before shared deadline") }
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
	if err != nil || !applied { t.Fatalf("schedule: %v", err) }
	old := *arming.Playback.Pending
	if _, ok := s.ExpireArming(host.RoomID, old.ID); ok { t.Fatal("premature ARM expiry") }
	if _, _, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, old.TrackGeneration, old.ID); err != nil {
		t.Fatal(err)
	}
	// Inject an elapsed ARM deadline instead of sleeping five seconds in CI.
	s.mu.Lock()
	s.rooms[host.RoomID].playback.Pending.ArmExpiresServerMs = s.ServerMs()-1
	s.mu.Unlock()
	canceled, expired := s.ExpireArming(host.RoomID, old.ID)
	if !expired || canceled.Playback.Pending != nil || canceled.Playback.PlayState != "paused" {
		t.Fatalf("unconfirmed ARM did not fail closed: %+v", canceled)
	}
	if _, ok := s.CommitEvent(host.RoomID, old.ID); ok { t.Fatal("expired event committed") }
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, old.TrackGeneration, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale ACK revived expired event: %v", err)
	}
}

func TestCancelOrReplaceArmingRevokesOldAcks(t *testing.T) {
	s, host, guest, hash := preparedRoom(t)
	start, _ := s.Snapshot(host.RoomID, host.SessionToken)
	p, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-cancel-1", start.StateRevision)
	if err != nil { t.Fatal(err) }
	old := *p.Playback.Pending
	if _, _, err := s.AcknowledgeArming(host.RoomID, host.SessionToken, hash, 1, old.ID); err != nil { t.Fatal(err) }
	canceled, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, false)
	if err != nil || canceled.Playback.Pending != nil { t.Fatalf("Ready revoked but event survived: %v", err) }
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale ACK accepted after cancellation: %v", err)
	}
	if _, err := s.SetReady(host.RoomID, guest.SessionToken, hash, 1, true); err != nil { t.Fatal(err) }
	now, _ := s.Snapshot(host.RoomID, host.SessionToken)
	newEvent, _, err := s.ControlSchedule(host.RoomID, host.SessionToken, "play", 0, "arm-replace-2", now.StateRevision)
	if err != nil || newEvent.Playback.Pending.ID == old.ID { t.Fatal("replacement did not change event ID") }
	if _, _, err := s.AcknowledgeArming(host.RoomID, guest.SessionToken, hash, 1, old.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("old ACK accepted after event replacement: %v", err)
	}
}
