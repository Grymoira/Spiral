package rooms

import (
	"strings"
	"testing"
	"time"
)

func TestAuthorityReadyAndStaleSchedule(t *testing.T) {
	s := NewStore()
	host, e := s.Create()
	if e != nil {
		t.Fatal(e)
	}
	guest, e := s.Join(host.RoomID, host.InviteCode)
	if e != nil {
		t.Fatal(e)
	}
	hash := strings.Repeat("a", 64)
	if _, e = s.SetTrack(host.RoomID, guest.SessionToken, hash, 20000); e != ErrForbidden {
		t.Fatalf("member changed track: %v", e)
	}
	state, e := s.SetTrack(host.RoomID, host.SessionToken, hash, 20000)
	if e != nil {
		t.Fatal(e)
	}
	if state.Playback.TrackGeneration != 1 {
		t.Fatal("track generation")
	}
	if _, e = s.Schedule(host.RoomID, host.SessionToken, "play", 0); e != ErrNotReady {
		t.Fatalf("ready barrier %v", e)
	}
	if _, e = s.SetReady(host.RoomID, guest.SessionToken, hash, 1, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetReady(host.RoomID, host.SessionToken, hash, 1, true); e != nil {
		t.Fatal(e)
	}
	first, e := s.Schedule(host.RoomID, host.SessionToken, "play", 0)
	if e != nil {
		t.Fatal(e)
	}
	id := first.Playback.Pending.ID
	next, e := s.Schedule(host.RoomID, host.SessionToken, "seek", 5000)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := s.CommitEvent(host.RoomID, id); ok {
		t.Fatal("stale event committed")
	}
	if !s.PendingMatches(host.RoomID, next.Playback.Pending.ID) {
		t.Fatal("next event missing")
	}
	if _, e = s.SetTrack(host.RoomID, host.SessionToken, strings.Repeat("b", 64), 10000); e != nil {
		t.Fatal(e)
	}
	if s.PendingMatches(host.RoomID, next.Playback.Pending.ID) {
		t.Fatal("changed track retained pending")
	}
	if _, e = s.SetReady(host.RoomID, guest.SessionToken, hash, 1, true); e != ErrInvalidState {
		t.Fatalf("old track was ready: %v", e)
	}
}
func TestScheduleCommitAndMediaProjection(t *testing.T) {
	s := NewStore()
	host, _ := s.Create()
	hash := strings.Repeat("a", 64)
	s.SetTrack(host.RoomID, host.SessionToken, hash, 10000)
	s.SetReady(host.RoomID, host.SessionToken, hash, 1, true)
	scheduled, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0)
	if err != nil {
		t.Fatal(err)
	}
	event := scheduled.Playback.Pending
	if _, ok := s.CommitEvent(host.RoomID, event.ID); ok {
		t.Fatal("premature commit")
	}
	time.Sleep(time.Duration(event.ExecuteServerMs-s.ServerMs()+2) * time.Millisecond)
	applied, ok := s.CommitEvent(host.RoomID, event.ID)
	if !ok || applied.Playback.PlayState != "playing" || applied.Playback.Pending != nil {
		t.Fatal("event not committed")
	}
	if expected := mediaAt(applied.Playback, applied.Playback.AnchorServerMs+200); expected != 200 {
		t.Fatalf("projection: %d", expected)
	}
}

func TestPendingPlayCancelledWhenReadinessRevoked(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	guest, err := s.Join(host.RoomID, host.InviteCode)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("c", 64)
	track, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 30000)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{host.SessionToken, guest.SessionToken} {
		if _, err := s.SetReady(host.RoomID, token, hash, track.Playback.TrackGeneration, true); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0)
	if err != nil {
		t.Fatal(err)
	}
	id := pending.Playback.Pending.ID
	revoked, err := s.SetReady(host.RoomID, guest.SessionToken, hash, track.Playback.TrackGeneration, false)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Playback.Pending != nil || revoked.StateRevision <= pending.StateRevision {
		t.Fatalf("unready participant did not cancel pending play: %+v", revoked)
	}
	if s.PendingMatches(host.RoomID, id) {
		t.Fatal("stale play is still pending")
	}
	if _, committed := s.CommitEvent(host.RoomID, id); committed {
		t.Fatal("stale play committed")
	}
	if _, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0); err != ErrNotReady {
		t.Fatalf("unready participant bypassed barrier: %v", err)
	}
}

func TestNewMemberCancelsPendingPlayback(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("d", 64)
	if _, err := s.SetTrack(host.RoomID, host.SessionToken, hash, 20000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetReady(host.RoomID, host.SessionToken, hash, 1, true); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Schedule(host.RoomID, host.SessionToken, "play", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Join(host.RoomID, host.InviteCode); err != nil {
		t.Fatal(err)
	}
	current, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if current.Playback.Pending != nil || current.Playback.PlayState != "paused" {
		t.Fatalf("new unready participant retained pending play: %+v", current.Playback)
	}
	if _, committed := s.CommitEvent(host.RoomID, pending.Playback.Pending.ID); committed {
		t.Fatal("scheduled play committed after new participant joined")
	}
}

func TestPendingPauseUnaffectedByUnreadyMember(t *testing.T) {
	s := NewStore()
	host, err := s.Create()
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("e", 64)
	s.SetTrack(host.RoomID, host.SessionToken, hash, 20000)
	s.SetReady(host.RoomID, host.SessionToken, hash, 1, true)
	pause, err := s.Schedule(host.RoomID, host.SessionToken, "pause", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Join(host.RoomID, host.InviteCode); err != nil {
		t.Fatal(err)
	}
	current, err := s.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if current.Playback.Pending == nil || current.Playback.Pending.ID != pause.Playback.Pending.ID {
		t.Fatal("safe pause must remain scheduled when a member joins")
	}
}
