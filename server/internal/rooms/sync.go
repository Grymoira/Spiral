package rooms

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrNoTrack      = errors.New("no track selected")
	ErrNotReady     = errors.New("room members are not ready")
	ErrInvalidState = errors.New("invalid sync state")
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const maxTrackDurationMs int64 = 24 * 60 * 60 * 1000
const scheduleLeadMs int64 = 2500

type Playback struct {
	TrackID         string          `json:"trackId"`
	TrackGeneration uint64          `json:"trackGeneration"`
	DurationMs      int64           `json:"durationMs"`
	PlayState       string          `json:"playState"`
	AnchorMediaMs   int64           `json:"anchorMediaMs"`
	AnchorServerMs  int64           `json:"anchorServerMs"`
	Pending         *ScheduledEvent `json:"pending"`
}

type ScheduledEvent struct {
	ID              uint64 `json:"id"`
	Action          string `json:"action"`
	TrackGeneration uint64 `json:"trackGeneration"`
	ExecuteServerMs int64  `json:"executeServerMs"`
	MediaMs         int64  `json:"mediaMs"`
	PlayState       string `json:"playState"`
}

// ServerMs uses a process-local monotonic epoch, distinct from wall clock timestamps.
func (s *Store) ServerMs() int64    { return time.Since(s.started).Milliseconds() }
func (s *Store) ClockEpoch() string { return s.clockEpoch }

func (s *Store) authorized(roomID, token string) (*room, membership, error) {
	r, ok := s.rooms[roomID]
	if !ok {
		return nil, membership{}, ErrNotFound
	}
	if !s.purgeExpired(r) {
		return nil, membership{}, ErrNotFound
	}
	m, ok := r.members[token]
	if !ok {
		return nil, membership{}, ErrUnauthorized
	}
	return r, m, nil
}

func (s *Store) Authorize(roomID, token string) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, m, err := s.authorized(roomID, token)
	if err != nil {
		return Member{}, err
	}
	return m.member, nil
}

func (s *Store) SetTrack(roomID, token, hash string, durationMs int64) (Snapshot, error) {
	if !sha256Pattern.MatchString(hash) || durationMs <= 0 || durationMs > maxTrackDurationMs {
		return Snapshot{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, m, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, err
	}
	if m.member.Role != "host" {
		return Snapshot{}, ErrForbidden
	}
	r.playback.TrackID = hash
	r.playback.DurationMs = durationMs
	r.playback.TrackGeneration++
	r.playback.PlayState = "paused"
	r.playback.AnchorMediaMs = 0
	r.playback.AnchorServerMs = s.ServerMs()
	r.playback.Pending = nil
	for k, member := range r.members {
		member.member.Ready = false
		r.members[k] = member
	}
	r.revision++
	return s.snapshot(r), nil
}

func (s *Store) SetReady(roomID, token, trackID string, generation uint64, ready bool) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, m, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, err
	}
	if r.playback.TrackID == "" || trackID != r.playback.TrackID || generation != r.playback.TrackGeneration {
		return Snapshot{}, ErrInvalidState
	}
	if m.member.Ready == ready {
		return s.snapshot(r), nil
	}
	m.member.Ready = ready
	r.members[token] = m
	r.revision++
	return s.snapshot(r), nil
}

func mediaAt(p Playback, atMs int64) int64 {
	media := p.AnchorMediaMs
	if p.PlayState == "playing" && atMs > p.AnchorServerMs {
		media += atMs - p.AnchorServerMs
	}
	if media < 0 {
		media = 0
	}
	if media > p.DurationMs {
		media = p.DurationMs
	}
	return media
}

func (s *Store) Schedule(roomID, token, action string, mediaMs int64) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, m, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, err
	}
	if m.member.Role != "host" {
		return Snapshot{}, ErrForbidden
	}
	if r.playback.TrackID == "" {
		return Snapshot{}, ErrNoTrack
	}
	if action != "play" && action != "pause" && action != "seek" {
		return Snapshot{}, ErrInvalidState
	}
	if mediaMs < 0 || mediaMs > r.playback.DurationMs {
		return Snapshot{}, ErrInvalidState
	}
	next := "paused"
	if action == "play" || (action == "seek" && r.playback.PlayState == "playing") {
		next = "playing"
	}
	if next == "playing" {
		for _, member := range r.members {
			if !member.member.Ready {
				return Snapshot{}, ErrNotReady
			}
		}
	}
	at := s.ServerMs() + scheduleLeadMs
	if action != "seek" {
		mediaMs = mediaAt(r.playback, at)
	}
	r.nextEvent++
	r.playback.Pending = &ScheduledEvent{
		ID: r.nextEvent, Action: action, TrackGeneration: r.playback.TrackGeneration,
		ExecuteServerMs: at, MediaMs: mediaMs, PlayState: next,
	}
	r.revision++
	return s.snapshot(r), nil
}

// CommitEvent is idempotent and refuses stale/replaced events.
func (s *Store) CommitEvent(roomID string, eventID uint64) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[roomID]
	if !ok || !s.purgeExpired(r) {
		return Snapshot{}, false
	}
	e := r.playback.Pending
	if e == nil || e.ID != eventID || e.TrackGeneration != r.playback.TrackGeneration || s.ServerMs() < e.ExecuteServerMs {
		return Snapshot{}, false
	}
	r.playback.PlayState = e.PlayState
	r.playback.AnchorMediaMs = e.MediaMs
	r.playback.AnchorServerMs = e.ExecuteServerMs
	r.playback.Pending = nil
	r.revision++
	return s.snapshot(r), true
}

// PendingMatches determines whether a delayed callback still belongs to the
// current track and scheduled command (e.g. when rapidly replacing events).
func (s *Store) PendingMatches(roomID string, eventID uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[roomID]
	return r != nil && r.playback.Pending != nil && r.playback.Pending.ID == eventID
}
