package realtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

type envelope struct {
	Type            string `json:"type"`
	RoomID          string `json:"roomId,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
	ProbeID         string `json:"probeId,omitempty"`
	C0              int64  `json:"c0,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	DurationMs      int64  `json:"durationMs,omitempty"`
	TrackID         string `json:"trackId,omitempty"`
	TrackGeneration uint64 `json:"trackGeneration,omitempty"`
	Ready           bool   `json:"ready,omitempty"`
	Action          string `json:"action,omitempty"`
	MediaMs         int64  `json:"mediaMs,omitempty"`
}

type peer struct {
	conn     *socket
	roomID   string
	memberID string
	token    string
	outgoing chan []byte
	shutdown chan struct{}
	once     sync.Once
}

func (p *peer) close() { p.once.Do(func() { close(p.shutdown); _ = p.conn.Close() }) }
func (p *peer) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	// Hub sends may race with disconnection. Protect from closing channel by
	// leaving the queue open; socket closure stops the writer on error.
	select {
	case <-p.shutdown:
		return
	case p.outgoing <- data:
	default:
		p.close()
	}
}

type Hub struct {
	logger             *slog.Logger
	connectionSequence atomic.Uint64
	Store              *rooms.Store
	mu                 sync.Mutex
	clients            map[string]map[string]*peer
	timerMu            sync.Mutex
	timers             map[string]*time.Timer
	admitMu            sync.Mutex
	activeSockets      int
	socketsByIP        map[string]int
}

func NewHub(s *rooms.Store) *Hub {
	return &Hub{Store: s, logger: slog.Default(), clients: make(map[string]map[string]*peer), timers: make(map[string]*time.Timer), socketsByIP: make(map[string]int)}
}

// These are protective budgets for unauthenticated and authenticated sockets,
// not a substitute for a front-end rate limiter or a production load test.
const maxOpenWebSockets = 512
const maxWebSocketsPerIP = 64

func (h *Hub) acquireSocket(remote string) (func(), bool) {
	ip, _, err := netSplitHostPort(remote)
	if err != nil || ip == "" {
		return nil, false
	}
	h.admitMu.Lock()
	defer h.admitMu.Unlock()
	if h.activeSockets >= maxOpenWebSockets || h.socketsByIP[ip] >= maxWebSocketsPerIP {
		return nil, false
	}
	h.activeSockets++
	h.socketsByIP[ip]++
	return func() {
		h.admitMu.Lock()
		defer h.admitMu.Unlock()
		h.activeSockets--
		h.socketsByIP[ip]--
		if h.socketsByIP[ip] == 0 {
			delete(h.socketsByIP, ip)
		}
	}, true
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connectionID := fmt.Sprintf("ws-%016x", h.connectionSequence.Add(1))
	diagnostic := h.logger.With("component", "websocket", "connection_id", connectionID)

	if r.URL.RawQuery != "" {
		diagnostic.Warn("websocket.handshake_rejected", "reason", "query_forbidden")
		http.Error(w, "websocket credentials must not be in URL", http.StatusBadRequest)
		return
	}
	if r.TLS == nil && !loopbackRequest(r) {
		diagnostic.Warn("websocket.handshake_rejected", "reason", "unencrypted_remote")
		http.Error(w, "plaintext websocket is only permitted over loopback", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		diagnostic.Warn("websocket.handshake_rejected", "reason", "invalid_method")
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	release, allowed := h.acquireSocket(r.RemoteAddr)
	if !allowed {
		diagnostic.Warn("websocket.admission_rejected", "reason", "connection_limit")
		w.Header().Set("Retry-After", "2")
		http.Error(w, "websocket capacity reached", http.StatusTooManyRequests)
		return
	}
	defer release()
	c, err := upgrade(w, r)
	if err != nil {
		diagnostic.Warn("websocket.handshake_rejected", "reason", "invalid_handshake")
		http.Error(w, "invalid websocket handshake", http.StatusBadRequest)
		return
	}
	diagnostic.Info("websocket.connected")
	defer diagnostic.Info("websocket.disconnected")
	h.handle(c, diagnostic)
}

func loopbackRequest(r *http.Request) bool {
	// Plain WS is allowed only if the listener is bound to loopback, enforced by main.
	// In addition verify the remote peer is loopback (e.g. reverse proxy cannot
	// silently expose the development transport).
	host, _, err := netSplitHostPort(r.RemoteAddr)
	return err == nil && parseIPLoopback(host)
}

// Helpers declared in transport_security.go to keep connection checks testable.

func strictEnvelope(data []byte) (envelope, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return envelope{}, errors.New("root is not object")
	}
	seen := map[string]bool{}
	fields := map[string]json.RawMessage{}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return envelope{}, err
		}
		key, ok := k.(string)
		if !ok || seen[key] {
			return envelope{}, errors.New("duplicate field")
		}
		seen[key] = true
		// Fields must use protocol-exact lowerCamelCase names.
		switch key {
		case "type", "roomId", "sessionToken", "probeId", "c0", "sha256", "durationMs", "trackId", "trackGeneration", "ready", "action", "mediaMs":
		default:
			return envelope{}, errors.New("unknown field")
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return envelope{}, err
		}
		fields[key] = raw
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return envelope{}, errors.New("invalid object")
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return envelope{}, errors.New("trailing content")
	}
	d2 := json.NewDecoder(bytes.NewReader(data))
	d2.DisallowUnknownFields()
	var m envelope
	if err := d2.Decode(&m); err != nil {
		return envelope{}, err
	}
	allowed := map[string][]string{
		"auth":        {"type", "roomId", "sessionToken"},
		"clock_probe": {"type", "probeId", "c0"},
		"track":       {"type", "sha256", "durationMs"},
		"ready":       {"type", "trackId", "trackGeneration", "ready"},
		"schedule":    {"type", "action", "mediaMs"},
		"snapshot":    {"type"},
	}
	keys, ok := allowed[m.Type]
	if !ok {
		return envelope{}, errors.New("unknown action")
	}
	accepted := map[string]bool{}
	for _, key := range keys {
		accepted[key] = true
	}
	for k := range fields {
		if !accepted[k] {
			return envelope{}, errors.New("unexpected field for action")
		}
	}
	return m, nil
}

func (h *Hub) register(p *peer) {
	h.mu.Lock()
	if h.clients[p.roomID] == nil {
		h.clients[p.roomID] = map[string]*peer{}
	}
	old := h.clients[p.roomID][p.memberID]
	h.clients[p.roomID][p.memberID] = p
	// Replacement and readiness revocation must be atomic with commands.
	h.clearReady(p)
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// isCurrent fences out a superseded connection, even if a buffered frame was
// already received before its socket was closed by a reconnect.
func (h *Hub) isCurrent(p *peer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clients[p.roomID][p.memberID] == p
}

func (h *Hub) unregister(p *peer) {
	h.mu.Lock()
	current := h.clients[p.roomID][p.memberID] == p
	if current {
		delete(h.clients[p.roomID], p.memberID)
	}
	if len(h.clients[p.roomID]) == 0 {
		delete(h.clients, p.roomID)
	}
	if current {
		h.clearReady(p)
	}
	h.mu.Unlock()
	if current {
		h.Broadcast(p.roomID)
	}
}
// clearReady is called with Hub.mu held, before registration or removal completes.
// Lock ordering throughout Hub: Hub.mu before Store.mu.
func (h *Hub) clearReady(p *peer) {
	state, err := h.Store.Snapshot(p.roomID, p.token)
	if err == nil && state.Playback.TrackID != "" {
		_, _ = h.Store.SetReady(p.roomID, p.token, state.Playback.TrackID, state.Playback.TrackGeneration, false)
	}
}
func (h *Hub) Broadcast(roomID string) {
	// No token or secret is included in broadcast snapshots.
	h.mu.Lock()
	ps := make([]*peer, 0, len(h.clients[roomID]))
	for _, p := range h.clients[roomID] {
		ps = append(ps, p)
	}
	h.mu.Unlock()
	for _, p := range ps {
		snap, err := h.Store.Snapshot(roomID, p.token)
		if err != nil {
			p.close()
			continue
		}
		p.send(map[string]any{"type": "snapshot", "state": snap})
	}
}

func (h *Hub) cancelTimer(roomID string) {
	h.timerMu.Lock()
	defer h.timerMu.Unlock()
	if old := h.timers[roomID]; old != nil {
		old.Stop()
		delete(h.timers, roomID)
	}
}

func (h *Hub) scheduleTimer(roomID string, snap rooms.Snapshot) {
	if snap.Playback.Pending == nil {
		return
	}
	event := *snap.Playback.Pending
	delta := time.Duration(event.ExecuteServerMs-h.Store.ServerMs()) * time.Millisecond
	if delta < 0 {
		delta = 0
	}
	h.timerMu.Lock()
	if !h.Store.PendingMatches(roomID, event.ID) {
		h.timerMu.Unlock()
		return
	}
	if previous := h.timers[roomID]; previous != nil {
		previous.Stop()
	}
	var pendingTimer *time.Timer
	pendingTimer = time.AfterFunc(delta+time.Millisecond, func() {
		h.timerMu.Lock()
		if h.timers[roomID] == pendingTimer {
			delete(h.timers, roomID)
		}
		h.timerMu.Unlock()
		if _, ok := h.Store.CommitEvent(roomID, event.ID); ok {
			h.logger.Info("playback.committed", "component", "synchronization", "event_id", event.ID, "action", event.Action, "track_generation", event.TrackGeneration)
			h.Broadcast(roomID)
		}
	})
	h.timers[roomID] = pendingTimer
	h.timerMu.Unlock()
}

var errSuperseded = errors.New("session superseded")

// applyCommand holds the same lock from the current-peer check through the
// authoritative Store mutation. A replacement cannot slip between them.
// Lock ordering: Hub.mu -> Store.mu. Never broadcast while holding Hub.mu.
func (h *Hub) applyCommand(p *peer, cmd envelope) (rooms.Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[p.roomID][p.memberID] != p {
		return rooms.Snapshot{}, errSuperseded
	}
	// Also recheck REST leave/session expiry, which can revoke WS authority.
	if _, err := h.Store.Authorize(p.roomID, p.token); err != nil {
		return rooms.Snapshot{}, err
	}
	switch cmd.Type {
	case "track":
		return h.Store.SetTrack(p.roomID, p.token, cmd.SHA256, cmd.DurationMs)
	case "ready":
		return h.Store.SetReady(p.roomID, p.token, cmd.TrackID, cmd.TrackGeneration, cmd.Ready)
	case "schedule":
		return h.Store.Schedule(p.roomID, p.token, cmd.Action, cmd.MediaMs)
	case "clock_probe":
		return rooms.Snapshot{}, nil // No mutation; reply after releasing Hub.mu.
	default:
		return rooms.Snapshot{}, errors.New("command not supported")
	}
}

func (h *Hub) handle(c *socket, diagnostic *slog.Logger) {
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(8 * time.Second))
	message, err := c.readText()
	if err != nil {
		return
	}
	m, err := strictEnvelope(message)
	if err != nil || m.Type != "auth" || m.RoomID == "" || m.SessionToken == "" {
		diagnostic.Warn("websocket.authentication_rejected", "reason", "malformed_auth")
		_ = c.writeFrame(1, []byte(`{"type":"error","code":"auth_required"}`))
		return
	}
	member, err := h.Store.Authorize(m.RoomID, m.SessionToken)
	if err != nil {
		diagnostic.Warn("websocket.authentication_rejected", "reason", "unauthorized")
		_ = c.writeFrame(1, []byte(`{"type":"error","code":"unauthorized"}`))
		return
	}
	diagnostic.Info("websocket.authenticated", "role", member.Role)
	p := &peer{conn: c, roomID: m.RoomID, memberID: member.ID, token: m.SessionToken, outgoing: make(chan []byte, 32), shutdown: make(chan struct{})}
	h.register(p)
	defer h.unregister(p)
	// One writer goroutine; writes are also serialized with protocol control replies.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-p.shutdown:
				return
			case data := <-p.outgoing:
				if err := c.writeFrame(1, data); err != nil {
					p.close()
					return
				}
			}
		}
	}()
	defer func() { p.close(); <-done }()
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		defer close(heartbeatDone)
		for {
			select {
			case <-p.shutdown:
				return
			case <-ticker.C:
				if err := c.writeFrame(9, []byte("ping")); err != nil {
					p.close()
					return
				}
			}
		}
	}()
	defer func() { p.close(); <-heartbeatDone }()
	p.send(map[string]any{"type": "auth_ok", "memberId": member.ID, "role": member.Role, "clockEpoch": h.Store.ClockEpoch()})
	h.Broadcast(m.RoomID)
	windowStart := time.Now()
	count := 0
	for {
		_ = c.SetReadDeadline(time.Now().Add(75 * time.Second))
		raw, err := c.readText()
		if err != nil {
			return
		}
		if time.Since(windowStart) > 10*time.Second {
			windowStart = time.Now()
			count = 0
		}
		count++
		if count > 80 {
			diagnostic.Warn("websocket.rate_limited")
			p.send(map[string]string{"type": "error", "code": "rate_limited"})
			return
		}
		cmd, err := strictEnvelope(raw)
		if err != nil {
			diagnostic.Warn("websocket.invalid_message", "reason", "schema")
			p.send(map[string]string{"type": "error", "code": "invalid_message"})
			continue
		}
		snap, err := h.applyCommand(p, cmd)
		if errors.Is(err, errSuperseded) {
			diagnostic.Warn("websocket.session_superseded")
			return
		}
		if errors.Is(err, rooms.ErrUnauthorized) || errors.Is(err, rooms.ErrNotFound) {
			diagnostic.Warn("websocket.session_revoked")
			p.send(map[string]string{"type": "error", "code": "unauthorized"})
			return
		}
		switch cmd.Type {
		case "clock_probe":
			if len(cmd.ProbeID) == 0 || len(cmd.ProbeID) > 64 || cmd.C0 < 0 {
				p.send(map[string]string{"type": "error", "code": "invalid_probe"})
				continue
			}
			s1 := h.Store.ServerMs()
			p.send(map[string]any{"type": "clock_reply", "probeId": cmd.ProbeID, "c0": cmd.C0, "s1": s1, "s2": h.Store.ServerMs(), "clockEpoch": h.Store.ClockEpoch()})
		case "track":
			if err == nil {
				diagnostic.Info("playback.track_changed", "duration_ms", cmd.DurationMs)
				h.cancelTimer(p.roomID)
				h.Broadcast(p.roomID)
			}
		case "ready":
			if err == nil {
				diagnostic.Info("playback.readiness_changed", "ready", cmd.Ready)
				h.Broadcast(p.roomID)
			}
		case "schedule":
			if err == nil {
				diagnostic.Info("playback.scheduled", "action", cmd.Action, "event_id", snap.Playback.Pending.ID, "track_generation", snap.Playback.TrackGeneration)
				h.Broadcast(p.roomID)
				h.scheduleTimer(p.roomID, snap)
			}
		}
		if err != nil {
			diagnostic.Warn("websocket.operation_rejected", "reason", errorCode(err), "action", cmd.Type)
			p.send(map[string]string{"type": "error", "code": errorCode(err)})
		}
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, rooms.ErrForbidden):
		return "forbidden"
	case errors.Is(err, rooms.ErrNotReady):
		return "not_ready"
	case errors.Is(err, rooms.ErrNoTrack):
		return "no_track"
	case errors.Is(err, rooms.ErrInvalidState):
		return "invalid_state"
	case errors.Is(err, rooms.ErrUnauthorized):
		return "unauthorized"
	default:
		return "operation_failed"
	}
}
