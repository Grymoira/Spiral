// Package rooms provides an in-memory, concurrency-safe room membership baseline.
// It does not implement the authoritative audio timeline or remote playback.
package rooms

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

const (
	MaxMembers      = 5
	MaxRooms        = 1024 // Bound memory while rooms are ephemeral.
	sessionLifetime = 24 * time.Hour
)

var (
	ErrNotFound     = errors.New("room not found")
	ErrForbidden    = errors.New("invalid room invitation")
	ErrUnauthorized = errors.New("invalid or expired session")
	ErrFull         = errors.New("room is full")
	ErrBusy         = errors.New("room limit reached")
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
	id        string
	invite    string
	revision  uint64
	epoch     string
	nextEvent uint64
	playback  Playback
	members   map[string]membership // Key is an unpredictable session token.
}

type Store struct {
	mu         sync.Mutex
	rooms      map[string]*room
	now        func() time.Time
	started    time.Time
	clockEpoch string
}

func NewStore() *Store {
	epoch, err := randomToken(12)
	if err != nil {
		panic("cryptographic random unavailable")
	}
	return &Store{rooms: make(map[string]*room), now: time.Now, started: time.Now(), clockEpoch: epoch}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Store) Create() (Credentials, error) {
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
	expires := s.now().Add(sessionLifetime)
	r := &room{id: id, epoch: id, invite: invite, revision: 1, playback: Playback{PlayState: "paused"}, members: map[string]membership{
		token: {member: Member{ID: memberID, Role: "host"}, expiresAt: expires},
	}}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Reap expired hosts when creating new rooms so abandoned rooms cannot
	// accumulate without bound. No goroutine or unbounded background queue.
	for _, old := range s.rooms {
		s.purgeExpired(old)
	}
	if len(s.rooms) >= MaxRooms {
		return Credentials{}, ErrBusy
	}
	// Collision-resistant IDs are still checked rather than assumed unique.
	if _, exists := s.rooms[id]; exists {
		return Credentials{}, errors.New("room identifier collision")
	}
	s.rooms[id] = r
	return Credentials{RoomID: id, MemberID: memberID, SessionToken: token, SessionExpiresAt: expires, InviteCode: invite}, nil
}

// purgeExpired must be called while holding s.mu. An expired host destroys the room.
func (s *Store) purgeExpired(r *room) bool {
	for token, m := range r.members {
		if !s.now().Before(m.expiresAt) {
			if m.member.Role == "host" {
				delete(s.rooms, r.id)
				return false
			}
			delete(r.members, token)
			r.revision++
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
	// A newly joined member is not ready: cancel an in-flight play event.
	s.cancelUnreadyPlaybackLocked(r)
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
	delete(r.members, token)
	r.revision++
	return nil
}
