package realtime_test

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

type testConn struct {
	net.Conn
	rd              *bufio.Reader
	revision        uint64
	commandSequence uint64
}

func connect(t *testing.T, addr string) *testConn {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return negotiate(t, conn, addr)
}
func negotiate(t *testing.T, conn net.Conn, addr string) *testConn {
	t.Helper()
	rd := bufio.NewReader(conn)
	key := base64.StdEncoding.EncodeToString([]byte("0123456789ABCDEF"))
	fmt.Fprintf(conn, "GET /v1/ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n\r\n", addr, key)
	response, err := http.ReadResponse(rd, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		t.Fatalf("websocket status %d", response.StatusCode)
	}
	t.Cleanup(func() { conn.Close() })
	return &testConn{Conn: conn, rd: rd}
}
func (c *testConn) send(t *testing.T, msg any) {
	t.Helper()
	if command, ok := msg.(map[string]any); ok {
		if command["type"] == "track" || command["type"] == "schedule" {
			c.commandSequence++
			if _, present := command["operationId"]; !present {
				command["operationId"] = fmt.Sprintf("test-command-%d", c.commandSequence)
			}
			if _, present := command["expectedRevision"]; !present {
				command["expectedRevision"] = c.revision
			}
		}
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	c.sendRaw(t, b)
}
func (c *testConn) sendRaw(t *testing.T, b []byte) {
	t.Helper()
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		t.Fatal(err)
	}
	frame := []byte{0x81}
	if len(b) < 126 {
		frame = append(frame, 0x80|byte(len(b)))
	} else {
		frame = append(frame, 0x80|126, byte(len(b)>>8), byte(len(b)))
	}
	frame = append(frame, mask...)
	for i, v := range b {
		frame = append(frame, v^mask[i%4])
	}
	if _, err := c.Write(frame); err != nil {
		t.Fatal(err)
	}
}
func (c *testConn) recv(t *testing.T) map[string]any {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		h := make([]byte, 2)
		if _, err := io.ReadFull(c.rd, h); err != nil {
			t.Fatal(err)
		}
		n := int(h[1] & 0x7f)
		if n == 126 {
			b := make([]byte, 2)
			if _, err := io.ReadFull(c.rd, b); err != nil {
				t.Fatal(err)
			}
			n = int(binary.BigEndian.Uint16(b))
		}
		if n > 4096 {
			t.Fatalf("response too large: %d", n)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.rd, payload); err != nil {
			t.Fatal(err)
		}
		if h[0]&0xf == 9 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(payload, &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] == "snapshot" {
			if s, ok := m["state"].(map[string]any); ok {
				if revision, ok := s["stateRevision"].(float64); ok {
					c.revision = uint64(revision)
				}
			}
		}
		return m
	}
}
func (c *testConn) until(t *testing.T, kind string, accept func(map[string]any) bool) map[string]any {
	t.Helper()
	for i := 0; i < 15; i++ {
		m := c.recv(t)
		if m["type"] == kind && (accept == nil || accept(m)) {
			return m
		}
	}
	t.Fatalf("no %s message within 15 messages", kind)
	return nil
}
func httpRoom(t *testing.T, client *http.Client, url string, body string) rooms.Credentials {
	t.Helper()
	res, err := client.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 201 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create/join status %d: %s", res.StatusCode, b)
	}
	var creds rooms.Credentials
	if err := json.NewDecoder(res.Body).Decode(&creds); err != nil {
		t.Fatal(err)
	}
	return creds
}
func state(m map[string]any) map[string]any    { return m["state"].(map[string]any) }
func playback(m map[string]any) map[string]any { return state(m)["playback"].(map[string]any) }

func TestTwoClientRealWebSocketTimeline(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	guest := httpRoom(t, server.Client(), server.URL+"/v1/rooms/"+host.RoomID+"/join", `{"inviteCode":"`+host.InviteCode+`"}`)
	addr := strings.TrimPrefix(server.URL, "http://")
	h := connect(t, addr)
	g := connect(t, addr)
	h.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	g.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": guest.SessionToken})
	h.until(t, "auth_ok", nil)
	g.until(t, "auth_ok", nil)
	h.until(t, "snapshot", nil)
	g.until(t, "snapshot", nil)
	// Members cannot alter host-controlled timeline.
	g.send(t, map[string]any{"type": "track", "sha256": strings.Repeat("a", 64), "durationMs": 10000})
	if m := g.until(t, "error", nil); m["code"] != "forbidden" {
		t.Fatalf("member control not rejected: %v", m)
	}
	h.send(t, map[string]any{"type": "track", "sha256": strings.Repeat("a", 64), "durationMs": 10000})
	a := h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] != "" })
	if playback(a)["trackGeneration"].(float64) != 1 {
		t.Fatal("generation missing")
	}
	g.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] != "" })
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	if m := h.until(t, "error", nil); m["code"] != "not_ready" {
		t.Fatalf("barrier missing: %v", m)
	}
	h.send(t, map[string]any{"type": "ready", "trackId": strings.Repeat("a", 64), "trackGeneration": 1, "ready": true})
	h.until(t, "snapshot", func(m map[string]any) bool { return state(m)["stateRevision"].(float64) >= 4 })
	g.send(t, map[string]any{"type": "ready", "trackId": strings.Repeat("a", 64), "trackGeneration": 1, "ready": true})
	g.until(t, "snapshot", func(m map[string]any) bool { return state(m)["stateRevision"].(float64) >= 5 })
	// Optimistic host commands must use the latest authoritative revision.
	// The guest's Ready changed the room after the host's previous snapshot.
	h.until(t, "snapshot", func(m map[string]any) bool { return state(m)["stateRevision"].(float64) >= 5 })
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	pendingH := h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["pending"] != nil })
	pendingG := g.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["pending"] != nil })
	ph := playback(pendingH)["pending"].(map[string]any)
	pg := playback(pendingG)["pending"].(map[string]any)
	if ph["id"] != pg["id"] || ph["phase"] != "arming" || pg["phase"] != "arming" {
		t.Fatal("members did not receive the same ARM event")
	}
	eventID := uint64(ph["id"].(float64))
	h.send(t, map[string]any{"type": "arm_ack", "trackId": strings.Repeat("a", 64), "trackGeneration": 1, "eventId": eventID})
	// A single ACK must never open the barrier.
	beforeAck, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil || beforeAck.Playback.Pending == nil || beforeAck.Playback.Pending.Phase != "arming" {
		t.Fatalf("single ACK committed playback: %+v %v", beforeAck, err)
	}
	g.send(t, map[string]any{"type": "arm_ack", "trackId": strings.Repeat("a", 64), "trackGeneration": 1, "eventId": eventID})
	committedH := h.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "committed"
	})
	committedG := g.until(t, "snapshot", func(m map[string]any) bool {
		p := playback(m)["pending"]
		return p != nil && p.(map[string]any)["phase"] == "committed"
	})
	ch := playback(committedH)["pending"].(map[string]any)
	cg := playback(committedG)["pending"].(map[string]any)
	if ch["executeServerMs"] != cg["executeServerMs"] || ch["executeServerMs"].(float64) <= 0 {
		t.Fatal("different committed deadlines")
	}
	// Real commit after the fresh 2.5-second lead, broadcast to two TCP clients.
	finalH := h.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil && playback(m)["playState"] == "playing"
	})
	finalG := g.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil && playback(m)["playState"] == "playing"
	})
	if playback(finalH)["anchorServerMs"] != playback(finalG)["anchorServerMs"] {
		t.Fatal("different committed timeline")
	}
	h.send(t, map[string]any{"type": "clock_probe", "probeId": "p1", "c0": 15})
	clock := h.until(t, "clock_reply", nil)
	if clock["probeId"] != "p1" || clock["clockEpoch"] == "" {
		t.Fatal("clock probe error")
	}
	// Invalid field casing is rejected (not silently mapped to token).
	h.sendRaw(t, []byte(`{"type":"schedule","ACTION":"pause","mediaMs":0}`))
	if m := h.until(t, "error", nil); m["code"] != "invalid_message" {
		t.Fatal("accepted invalid message casing")
	}
	// A now-playing guest abruptly losing the socket forces the server's
	// authoritative timeline to pause for the remaining host.
	if err := g.Close(); err != nil { t.Fatal(err) }
	paused := h.until(t, "snapshot", func(m map[string]any) bool {
		return playback(m)["pending"] == nil && playback(m)["playState"] == "paused"
	})
	if playback(paused)["anchorMediaMs"].(float64) < 0 {
		t.Fatal("disconnection produced invalid frozen media position")
	}
}

func TestWebSocketBadAuthorizationAndFrame(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	creds := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	conn := connect(t, strings.TrimPrefix(server.URL, "http://"))
	conn.send(t, map[string]any{"type": "auth", "roomId": creds.RoomID, "sessionToken": "invalid"})
	if m := conn.until(t, "error", nil); m["code"] != "unauthorized" {
		t.Fatalf("accepted bad auth: %v", m)
	}
	// URL query credentials must be explicitly rejected, even for valid requests.
	r, err := http.Get(server.URL + "/v1/ws?sessionToken=x")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatalf("query credentials: HTTP %d", r.StatusCode)
	}
	res, err := server.Client().Post(server.URL+"/v1/rooms/"+creds.RoomID+"/join", "application/json", bytes.NewBufferString(`{"INVITECODE":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("case-insensitive JSON accepted: %d", res.StatusCode)
	}
}

func TestAuthenticatedTLSSocket(t *testing.T) {
	store := rooms.NewStore()
	testServer := httptest.NewTLSServer(api.NewHandler(store))
	defer testServer.Close()
	creds := httpRoom(t, testServer.Client(), testServer.URL+"/v1/rooms", `{}`)
	addr := strings.TrimPrefix(testServer.URL, "https://")
	config := testServer.Client().Transport.(*http.Transport).TLSClientConfig
	// TLS trust exception belongs to the temporary httptest certificate only.
	if config == nil {
		t.Fatal("missing test cert transport")
	}
	conn, err := tls.Dial("tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	c := negotiate(t, conn, addr)
	c.send(t, map[string]any{"type": "auth", "roomId": creds.RoomID, "sessionToken": creds.SessionToken})
	if m := c.until(t, "auth_ok", nil); m["role"] != "host" {
		t.Fatalf("TLS websocket auth failed: %v", m)
	}
}

func TestDisconnectCancelsScheduledPlayForRoom(t *testing.T) {
	store := rooms.NewStore()
	server := httptest.NewServer(api.NewHandler(store))
	defer server.Close()
	host := httpRoom(t, server.Client(), server.URL+"/v1/rooms", `{}`)
	guest := httpRoom(t, server.Client(), server.URL+"/v1/rooms/"+host.RoomID+"/join", `{"inviteCode":"`+host.InviteCode+`"}`)
	addr := strings.TrimPrefix(server.URL, "http://")
	h := connect(t, addr)
	g := connect(t, addr)
	h.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": host.SessionToken})
	g.send(t, map[string]any{"type": "auth", "roomId": host.RoomID, "sessionToken": guest.SessionToken})
	h.until(t, "auth_ok", nil)
	g.until(t, "auth_ok", nil)
	h.until(t, "snapshot", nil)
	g.until(t, "snapshot", nil)
	hash := strings.Repeat("f", 64)
	h.send(t, map[string]any{"type": "track", "sha256": hash, "durationMs": 20000})
	h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	g.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["trackId"] == hash })
	for _, c := range []*testConn{h, g} {
		c.send(t, map[string]any{"type": "ready", "trackId": hash, "trackGeneration": 1, "ready": true})
	}
	h.until(t, "snapshot", func(m map[string]any) bool {
		members := state(m)["members"].([]any)
		return len(members) == 2 && members[0].(map[string]any)["ready"] == true && members[1].(map[string]any)["ready"] == true
	})
	h.send(t, map[string]any{"type": "schedule", "action": "play", "mediaMs": 0})
	before := h.until(t, "snapshot", func(m map[string]any) bool { return playback(m)["pending"] != nil })
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	after := h.until(t, "snapshot", func(m map[string]any) bool {
		return state(m)["stateRevision"].(float64) > state(before)["stateRevision"].(float64) && playback(m)["pending"] == nil
	})
	if playback(after)["playState"] != "paused" {
		t.Fatal("play committed after guest disconnect")
	}
	// Let the original timer deadline elapse; a stale scheduled event cannot restart playback.
	time.Sleep(2600 * time.Millisecond)
	final, err := store.Snapshot(host.RoomID, host.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if final.Playback.PlayState != "paused" || final.Playback.Pending != nil {
		t.Fatalf("old timer restarted playback: %+v", final.Playback)
	}
}
