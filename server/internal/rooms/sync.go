package rooms

import (
	"errors"
	"regexp"
	"time"
)

var (
	ErrNoTrack           = errors.New("no track selected")
	ErrNotReady          = errors.New("room members are not ready")
	ErrInvalidState      = errors.New("invalid sync state")
	ErrRevisionConflict  = errors.New("room revision conflict")
	ErrOperationConflict = errors.New("operation identifier reused with different payload")
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const maxTrackDurationMs int64 = 24 * 60 * 60 * 1000
const scheduleLeadMs int64 = 2500
const armTimeoutMs int64 = 5000
const maxRememberedOperations = 64

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
	Phase           string `json:"phase"`
	ArmExpiresServerMs int64 `json:"armExpiresServerMs,omitempty"`
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
	r.claimed = true
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

// Accepted operation receipts are retained per room across WebSocket reconnects.
// The fixed retention cap bounds memory. Expired receipts cannot bypass revision
// checks: a retry with an evicted key and stale expectedRevision is rejected.
type controlReceipt struct {
	memberID         string
	kind             string
	expectedRevision uint64
	hash             string
	duration         int64
	reload           bool
	action           string
	mediaMs          int64
}

func (r *room) findOperation(id string, candidate controlReceipt) (bool, error) {
	old, present := r.operations[id]
	if !present {
		return false, nil
	}
	if old != candidate {
		return false, ErrOperationConflict
	}
	return true, nil
}

func (r *room) rememberOperation(id string, receipt controlReceipt) {
	if r.operations == nil {
		r.operations = make(map[string]controlReceipt)
	}
	if len(r.operationOrder) == maxRememberedOperations {
		delete(r.operations, r.operationOrder[0])
		r.operationOrder = r.operationOrder[1:]
	}
	r.operationOrder = append(r.operationOrder, id)
	r.operations[id] = receipt
}

// ControlTrack applies a host track command with atomic deduplication and
// optimistic revision checking. applied=false means neither state nor timer
// should be reset; accepted duplicate keys return the current room snapshot.
func (s *Store) ControlTrack(roomID, token, hash string, durationMs int64,
	operationID string, expectedRevision uint64, reload bool) (Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, member, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, false, err
	}
	if member.member.Role != "host" {
		return Snapshot{}, false, ErrForbidden
	}
	if operationID == "" || !sha256Pattern.MatchString(hash) ||
		durationMs <= 0 || durationMs > maxTrackDurationMs {
		return Snapshot{}, false, ErrInvalidState
	}
	receipt := controlReceipt{memberID: member.member.ID, kind: "track",
		expectedRevision: expectedRevision, hash: hash, duration: durationMs, reload: reload}
	replayed, err := r.findOperation(operationID, receipt)
	if err != nil {
		return Snapshot{}, false, err
	}
	if replayed {
		return s.snapshot(r), false, nil
	}
	if expectedRevision != r.revision {
		return Snapshot{}, false, ErrRevisionConflict
	}
	applied := s.setTrackLocked(r, hash, durationMs, reload)
	r.rememberOperation(operationID, receipt)
	return s.snapshot(r), applied, nil
}

// ControlSchedule guarantees a retransmitted schedule command never allocates
// another event ID or shifts a deadline, even if the first event committed.
func (s *Store) ControlSchedule(roomID, token, action string, mediaMs int64,
	operationID string, expectedRevision uint64) (Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, member, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, false, err
	}
	if member.member.Role != "host" {
		return Snapshot{}, false, ErrForbidden
	}
	if operationID == "" {
		return Snapshot{}, false, ErrInvalidState
	}
	receipt := controlReceipt{memberID: member.member.ID, kind: "schedule",
		expectedRevision: expectedRevision, action: action, mediaMs: mediaMs}
	replayed, err := r.findOperation(operationID, receipt)
	if err != nil {
		return Snapshot{}, false, err
	}
	if replayed {
		return s.snapshot(r), false, nil
	}
	if expectedRevision != r.revision {
		return Snapshot{}, false, ErrRevisionConflict
	}
	if err := s.scheduleLocked(r, action, mediaMs, true); err != nil {
		return Snapshot{}, false, err
	}
	r.rememberOperation(operationID, receipt)
	return s.snapshot(r), true, nil
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
	s.setTrackLocked(r, hash, durationMs, false)
	return s.snapshot(r), nil
}

// setTrackLocked never resets readiness for an identical non-reload track.
// Caller holds Store.mu; reload explicitly forces a new generation.
func (s *Store) setTrackLocked(r *room, hash string, durationMs int64, reload bool) bool {
	if !reload && r.playback.TrackID == hash && r.playback.DurationMs == durationMs {
		return false
	}
	r.playback.TrackID = hash
	r.playback.DurationMs = durationMs
	r.playback.TrackGeneration++
	r.playback.PlayState = "paused"
	r.playback.AnchorMediaMs = 0
	r.playback.AnchorServerMs = s.ServerMs()
	r.playback.Pending = nil
	r.armingAcks = nil
	for k, member := range r.members {
		member.member.Ready = false
		r.members[k] = member
	}
	r.revision++
	return true
}

// cancelUnreadyPlaybackLocked withdraws a queued playing-state transition
// whenever the readiness barrier no longer holds. Caller must hold s.mu.
func (s *Store) cancelUnreadyPlaybackLocked(r *room) bool {
	if r.playback.Pending == nil || r.playback.Pending.PlayState != "playing" {
		return false
	}
	for _, m := range r.members {
		if !m.member.Ready {
			r.playback.Pending = nil
			r.armingAcks = nil
			return true
		}
	}
	return false
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
	s.cancelUnreadyPlaybackLocked(r)
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
	if err := s.scheduleLocked(r, action, mediaMs, false); err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(r), nil
}

// scheduleLocked is shared with the protocol path guarded by expectedRevision.
// Caller holds Store.mu; never allocate an event for a rejected command.
func (s *Store) scheduleLocked(r *room, action string, mediaMs int64, requireArming bool) error {
	if r.playback.TrackID == "" {
		return ErrNoTrack
	}
	if action != "play" && action != "pause" && action != "seek" {
		return ErrInvalidState
	}
	if mediaMs < 0 || mediaMs > r.playback.DurationMs {
		return ErrInvalidState
	}
	next := "paused"
	if action == "play" || (action == "seek" && r.playback.PlayState == "playing") {
		next = "playing"
	}
	if next == "playing" {
		for _, member := range r.members {
			if !member.member.Ready {
				return ErrNotReady
			}
		}
	}
	at := s.ServerMs() + scheduleLeadMs
	if action != "seek" {
		mediaMs = mediaAt(r.playback, at)
	}
	r.nextEvent++
	event := &ScheduledEvent{
		ID: r.nextEvent, Action: action, TrackGeneration: r.playback.TrackGeneration,
		ExecuteServerMs: at, MediaMs: mediaMs, PlayState: next, Phase: "committed",
	}
	r.armingAcks = nil
	if requireArming && next == "playing" {
		// No executable deadline is shared before every member ACKs this event.
		event.Phase = "arming"
		event.ExecuteServerMs = 0
		event.ArmExpiresServerMs = s.ServerMs() + armTimeoutMs
		r.armingAcks = make(map[string]bool, len(r.members))
	}
	r.playback.Pending = event
	r.revision++
	return nil
}

// AcknowledgeArming is an event-scoped, member-authenticated barrier. Only
// the final distinct ACK creates the shared executable deadline.
func (s *Store) AcknowledgeArming(roomID, token, trackID string, generation, eventID uint64) (Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, member, err := s.authorized(roomID, token)
	if err != nil {
		return Snapshot{}, false, err
	}
	e := r.playback.Pending
	if e == nil || e.ID != eventID || e.TrackGeneration != generation ||
		trackID != r.playback.TrackID || e.PlayState != "playing" {
		return Snapshot{}, false, ErrInvalidState
	}
	if e.Phase == "committed" {
		return s.snapshot(r), false, nil
	}
	if e.Phase != "arming" || s.ServerMs() >= e.ArmExpiresServerMs || !member.member.Ready {
		return Snapshot{}, false, ErrInvalidState
	}
	if r.armingAcks[member.member.ID] {
		return s.snapshot(r), false, nil
	}
	r.armingAcks[member.member.ID] = true
	for _, m := range r.members {
		if !m.member.Ready || !r.armingAcks[m.member.ID] {
			return s.snapshot(r), false, nil
		}
	}
	e.Phase = "committed"
	e.ExecuteServerMs = s.ServerMs() + scheduleLeadMs
	e.ArmExpiresServerMs = 0
	r.armingAcks = nil
	r.revision++
	return s.snapshot(r), true, nil
}

// ExpireArming cancels an unacknowledged play before it becomes executable.
func (s *Store) ExpireArming(roomID string, eventID uint64) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[roomID]
	if r == nil || !s.purgeExpired(r) || r.playback.Pending == nil {
		return Snapshot{}, false
	}
	e := r.playback.Pending
	if e.ID != eventID || e.Phase != "arming" || s.ServerMs() < e.ArmExpiresServerMs {
		return Snapshot{}, false
	}
	r.playback.Pending = nil
	r.armingAcks = nil
	r.revision++
	return s.snapshot(r), true
}

// Timer setup must fence the entire event phase and deadline, not only its ID.
func (s *Store) PendingMatchesExact(roomID string, expected ScheduledEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rooms[roomID]
	if r == nil || r.playback.Pending == nil {
		return false
	}
	e := r.playback.Pending
	return e.ID == expected.ID && e.TrackGeneration == expected.TrackGeneration &&
		e.Phase == expected.Phase && e.ExecuteServerMs == expected.ExecuteServerMs &&
		e.ArmExpiresServerMs == expected.ArmExpiresServerMs
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
	if e == nil || e.Phase != "committed" || e.ID != eventID || e.TrackGeneration != r.playback.TrackGeneration || s.ServerMs() < e.ExecuteServerMs {
		return Snapshot{}, false
	}
	// Recheck the barrier immediately before commit, not only at scheduling.
	if s.cancelUnreadyPlaybackLocked(r) {
		r.revision++
		return s.snapshot(r), false
	}
	r.playback.PlayState = e.PlayState
	r.playback.AnchorMediaMs = e.MediaMs
	r.playback.AnchorServerMs = e.ExecuteServerMs
	r.playback.Pending = nil
	r.armingAcks = nil
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
