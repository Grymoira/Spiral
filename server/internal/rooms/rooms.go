// Package rooms provides an in-memory, concurrency-safe room membership baseline.
// It does not implement the authoritative audio timeline or remote playback.
package rooms

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/netip"
	"sync"
	"time"
)

const (
	MaxMembers      = 5
	MaxRooms        = 1024 // Bound memory while rooms are ephemeral.
	sessionLifetime = 24 * time.Hour

	// New, never-used rooms expire quickly so anonymous reservations cannot
	// hold the room budget for the full 24-hour session lifetime.
	unclaimedRoomLifetime = 15 * time.Minute

	// Accepted public API creations are bounded in one rolling minute. The
	// fixed global window also bounds the per-source rate-accounting memory.
	creationWindow          = time.Minute
	maxCreationsPerWindow   = 48
	maxCreationsPerSource   = 6
	maxActiveRoomsPerSource = 24
)

var (
	ErrNotFound      = errors.New("room not found")
	ErrForbidden     = errors.New("invalid room invitation")
	ErrUnauthorized  = errors.New("invalid or expired session")
	ErrFull          = errors.New("room is full")
	ErrBusy          = errors.New("room limit reached")
	ErrRateLimited   = errors.New("room creation rate limited")
	ErrSourceQuota   = errors.New("source room quota reached")
	ErrInvalidSource = errors.New("invalid client source")
)

type Member struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Ready bool   `json:"ready"`
}

type Snapshot struct {
	RoomID        string   `json:"roomId"`
	StateRevision uint64   `json:"stateRevision"`
	Members       []Member `json:"members"`
	RoomEpoch     string   `json:"roomEpoch"`
	ClockEpoch    string   `json:"clockEpoch"`
	ServerMs      int64    `json:"serverMs"`
	Playback      Playback `json:"playback"`
}

type Credentials struct {
	RoomID           string    `json:"roomId"`
	MemberID         string    `json:"memberId"`
	SessionToken     string    `json:"sessionToken"`
	SessionExpiresAt time.Time `json:"sessionExpiresAt"`
	InviteCode       string    `json:"inviteCode,omitempty"`
}

type membership struct {
	member    Member
	expiresAt time.Time
}

type room struct {
	id             string
	invite         string
	source         string
	createdAt      time.Time
	claimed        bool
	revision       uint64
	epoch          string
	nextEvent      uint64
	playback       Playback
	operations     map[string]controlReceipt
	operationOrder []string
	armingAcks     map[string]bool       // per-member acknowledgements for the current playing event
	members        map[string]membership // Key is an unpredictable session token.
}

type creationRecord struct {
	source string
	at     time.Time
}

type Store struct {
	mu    sync.Mutex
	rooms map[string]*room
	// The ring-equivalent rolling slice holds at most maxCreationsPerWindow
	// successful public room creations; there is no per-IP unbounded map.
	recentCreates []creationRecord
	now           func() time.Time
	started       time.Time
	clockEpoch    string
	// A coalesced wake-up, not a queue of identities or secrets. Producing a
	// signal while holding Store.mu must never block on Hub processing.
	expirySignal chan struct{}
}

func NewStore() *Store { return NewStoreWithClock(time.Now) }

// NewStoreWithClock permits deterministic session-expiry tests. Callers must
// supply a concurrency-safe clock; playback uses its own monotonic epoch.
func NewStoreWithClock(now func() time.Time) *Store {
	if now == nil {
		panic("room clock cannot be nil")
	}
	epoch, err := randomToken(12)
	if err != nil {
		panic("cryptographic random unavailable")
	}
	return &Store{rooms: make(map[string]*room), now: now, started: time.Now(),
		clockEpoch: epoch, expirySignal: make(chan struct{}, 1)}
}

// ExpirationNotifications wakes the sole realtime hub after Store operations
// prune expired members or destroy expired rooms. Receivers must fetch fresh
// authoritative snapshots; the signal itself contains no session information.
func (s *Store) ExpirationNotifications() <-chan struct{} { return s.expirySignal }

func (s *Store) notifyExpiryLocked() {
	select {
	case s.expirySignal <- struct{}{}:
	default:
	}
}

// SweepExpired proactively enforces credential deadlines while active sockets
// exist. It is safe to call concurrently with HTTP and WebSocket operations.
func (s *Store) SweepExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rooms {
		s.purgeExpired(r)
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create is retained for internal test/embedding use. Public HTTP creation must
// use CreateForSource so requests cannot bypass admission limits.
func (s *Store) Create() (Credentials, error) {
	return s.create("", false)
}

// CreateForSource enforces bounded public admission on the actual peer IP.
// It must not receive an untrusted Forwarded/X-Forwarded-For header value.
func (s *Store) CreateForSource(rawIP string) (Credentials, error) {
	ip, err := netip.ParseAddr(rawIP)
	if err != nil {
		return Credentials{}, ErrInvalidSource
	}
	return s.create(ip.Unmap().WithZone("").String(), true)
}

func (s *Store) create(source string, limited bool) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	// Always reclaim stale rooms before checking either global or source
	// capacity. No background cleanup goroutine or indefinite room reservation.
	for _, old := range s.rooms {
		s.purgeExpired(old)
	}
	if len(s.rooms) >= MaxRooms {
		return Credentials{}, ErrBusy
	}
	if limited {
		active := 0
		for _, r := range s.rooms {
			if r.source == source {
				active++
			}
		}
		if active >= maxActiveRoomsPerSource {
			return Credentials{}, ErrSourceQuota
		}

		// Expired entries are discarded atomically with rate accounting.
		recent := s.recentCreates[:0]
		perSource := 0
		for _, event := range s.recentCreates {
			if now.Sub(event.at) >= creationWindow {
				continue
			}
			recent = append(recent, event)
			if event.source == source {
				perSource++
			}
		}
		s.recentCreates = recent
		if len(recent) >= maxCreationsPerWindow || perSource >= maxCreationsPerSource {
			return Credentials{}, ErrRateLimited
		}
	}

	// Generate secrets only for admitted attempts. Keep insertion and quota
	// accounting under Store.mu so simultaneous requests cannot overbook.
	id, err := randomToken(16)
	if err != nil {
		return Credentials{}, err
	}
	invite, err := randomToken(24)
	if err != nil {
		return Credentials{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return Credentials{}, err
	}
	memberID, err := randomToken(16)
	if err != nil {
		return Credentials{}, err
	}
	if _, exists := s.rooms[id]; exists {
		return Credentials{}, errors.New("room identifier collision")
	}
	expires := now.Add(sessionLifetime)
	r := &room{id: id, epoch: id, invite: invite, source: source,
		createdAt: now, revision: 1, playback: Playback{PlayState: "paused"},
		members: map[string]membership{
			token: {member: Member{ID: memberID, Role: "host"}, expiresAt: expires},
		},
	}
	s.rooms[id] = r
	if limited {
		s.recentCreates = append(s.recentCreates, creationRecord{source: source, at: now})
	}
	return Credentials{RoomID: id, MemberID: memberID, SessionToken: token, SessionExpiresAt: expires, InviteCode: invite}, nil
}

// purgeExpired must be called while holding s.mu. An expired host destroys the room.
func (s *Store) purgeExpired(r *room) bool {
	if !r.claimed && !s.now().Before(r.createdAt.Add(unclaimedRoomLifetime)) {
		delete(s.rooms, r.id)
		s.notifyExpiryLocked()
		return false
	}
	for token, m := range r.members {
		if !s.now().Before(m.expiresAt) {
			if m.member.Role == "host" {
				delete(s.rooms, r.id)
				s.notifyExpiryLocked()
				return false
			}
			// Treat a departed live member as a synchronization loss
			// before removal makes the remaining Ready set look complete.
			s.pausePlaybackLocked(r)
			delete(r.members, token)
			r.revision++
			s.notifyExpiryLocked()
		}
	}
	return true
}

func (s *Store) Join(roomID, invite string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[roomID]
	if !ok {
		return Credentials{}, ErrNotFound
	}
	if !s.purgeExpired(r) {
		return Credentials{}, ErrNotFound
	}
	if len(invite) != len(r.invite) || subtle.ConstantTimeCompare([]byte(invite), []byte(r.invite)) != 1 {
		return Credentials{}, ErrForbidden
	}
	if len(r.members) >= MaxMembers {
		return Credentials{}, ErrFull
	}
	token, err := randomToken(32)
	if err != nil {
		return Credentials{}, err
	}
	memberID, err := randomToken(16)
	if err != nil {
		return Credentials{}, err
	}
	expires := s.now().Add(sessionLifetime)
	r.members[token] = membership{member: Member{ID: memberID, Role: "member"}, expiresAt: expires}
	r.claimed = true
	// A newly joined member is not ready: invalidate future play and
	// pause any currently audible authoritative timeline.
	s.cancelUnreadyPlaybackLocked(r)
	s.pausePlaybackLocked(r)
	r.revision++
	return Credentials{RoomID: roomID, MemberID: memberID, SessionToken: token, SessionExpiresAt: expires}, nil
}

func (s *Store) Snapshot(roomID, token string) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[roomID]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	if !s.purgeExpired(r) {
		return Snapshot{}, ErrNotFound
	}
	if _, ok := r.members[token]; !ok {
		return Snapshot{}, ErrUnauthorized
	}
	r.claimed = true
	return s.snapshot(r), nil
}

// snapshot must be called while holding s.mu.
func (s *Store) snapshot(r *room) Snapshot {
	snap := Snapshot{RoomID: r.id, RoomEpoch: r.epoch, ClockEpoch: s.clockEpoch, ServerMs: s.ServerMs(), Playback: r.playback, StateRevision: r.revision, Members: make([]Member, 0, len(r.members))}
	for _, m := range r.members {
		snap.Members = append(snap.Members, m.member)
	}
	// Stable ordering is useful for clients and reproducible tests.
	sortMembers(snap.Members)
	if snap.Playback.Pending != nil {
		e := *snap.Playback.Pending
		snap.Playback.Pending = &e
	}
	return snap
}

func sortMembers(m []Member) {
	for i := 1; i < len(m); i++ {
		for j := i; j > 0 && m[j].ID < m[j-1].ID; j-- {
			m[j], m[j-1] = m[j-1], m[j]
		}
	}
}

func (s *Store) Leave(roomID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rooms[roomID]
	if !ok {
		return ErrNotFound
	}
	if !s.purgeExpired(r) {
		return ErrNotFound
	}
	member, ok := r.members[token]
	if !ok {
		return ErrUnauthorized
	}
	if member.member.Role == "host" {
		delete(s.rooms, roomID)
		return nil
	}
	s.pausePlaybackLocked(r)
	delete(r.members, token)
	r.revision++
	return nil
}
