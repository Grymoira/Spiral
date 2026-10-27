package realtime

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func makeMaskedTextFrame(n int) []byte {
	payload := bytes.Repeat([]byte("x"), n)
	frame := []byte{0x81}
	if n < 126 {
		frame = append(frame, 0x80|byte(n))
	} else {
		frame = append(frame, 0xfe, byte(n>>8), byte(n))
	}
	mask := [4]byte{1, 2, 3, 4}
	frame = append(frame, mask[:]...)
	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}
	return frame
}

// Regression: a 127-byte payload uses the 16-bit extended length encoding.
func TestReadTextBoundaryLengths(t *testing.T) {
	for _, n := range []int{125, 126, 127, 128, 4096} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s := &socket{reader: bufio.NewReader(bytes.NewReader(makeMaskedTextFrame(n)))}
			got, err := s.readText()
			if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("x"), n)) {
				t.Fatalf("length %d: got len %d; error %v", n, len(got), err)
			}
		})
	}
}

func TestReadTextRejectsFrameOverLimit(t *testing.T) {
	s := &socket{reader: bufio.NewReader(bytes.NewReader(makeMaskedTextFrame(maxMessage + 1)))}
	if _, err := s.readText(); err == nil {
		t.Fatal("oversized websocket frame unexpectedly accepted")
	}
}

// deadlineProbeConn collects socket deadlines and outbound Pong frames.
// It is used only with an in-memory reader (no network goroutines needed).
type deadlineProbeConn struct {
	net.Conn
	deadlines []time.Time
	outgoing  bytes.Buffer
}

func (c *deadlineProbeConn) SetReadDeadline(deadline time.Time) error {
	c.deadlines = append(c.deadlines, deadline)
	return nil
}

func (c *deadlineProbeConn) SetWriteDeadline(time.Time) error { return nil }
func (c *deadlineProbeConn) Write(data []byte) (int, error) {
	return c.outgoing.Write(data)
}

func maskedControlFrame(opcode byte, payload []byte) []byte {
	frame := []byte{0x80 | opcode, 0x80 | byte(len(payload)), 1, 2, 3, 4}
	for i, b := range payload {
		frame = append(frame, b^byte(i%4+1))
	}
	return frame
}

func TestAuthenticatedControlFramesRefreshIdleDeadline(t *testing.T) {
	// Both a response to server Ping and a client-initiated Ping demonstrate
	// a live authenticated peer despite an absence of application messages.
	stream := append(maskedControlFrame(0xa, []byte("ping")),
		maskedControlFrame(0x9, []byte("client-heartbeat"))...)
	stream = append(stream, makeMaskedTextFrame(125)...)
	conn := &deadlineProbeConn{}
	s := &socket{Conn: conn, reader: bufio.NewReader(bytes.NewReader(stream))}
	initial := time.Now().Add(authenticatedIdleTimeout)
	if err := conn.SetReadDeadline(initial); err != nil {
		t.Fatal(err)
	}
	controls := []byte{}
	message, err := s.readTextWithControl(func(op byte, _ []byte) error {
		controls = append(controls, op)
		return conn.SetReadDeadline(time.Now().Add(authenticatedIdleTimeout))
	})
	if err != nil || len(message) != 125 {
		t.Fatalf("authenticated read failed: message=%d, err=%v", len(message), err)
	}
	if !bytes.Equal(controls, []byte{0xa, 0x9}) {
		t.Fatalf("control callbacks missing or reordered: %v", controls)
	}
	if len(conn.deadlines) != 3 ||
		conn.deadlines[2].Before(conn.deadlines[1]) ||
		conn.deadlines[1].Before(conn.deadlines[0]) {
		t.Fatalf("idle deadline not extended on each valid control frame: %+v", conn.deadlines)
	}
	// Responding to a valid client Ping is still required by RFC 6455.
	if got := conn.outgoing.Bytes(); len(got) < 2 || got[0] != 0x8a ||
		!bytes.Equal(got[2:], []byte("client-heartbeat")) {
		t.Fatalf("server failed to send proper unmasked Pong: %x", got)
	}
}

func TestControlFramesCannotExtendUnauthenticatedDeadline(t *testing.T) {
	stream := append(maskedControlFrame(0xa, []byte("ping")),
		maskedControlFrame(0x9, []byte("hello"))...)
	stream = append(stream, makeMaskedTextFrame(125)...)
	conn := &deadlineProbeConn{}
	s := &socket{Conn: conn, reader: bufio.NewReader(bytes.NewReader(stream))}
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	if _, err := s.readText(); err != nil {
		t.Fatal(err)
	}
	if len(conn.deadlines) != 1 {
		t.Fatalf("pre-auth control frames extended authentication deadline: %d", len(conn.deadlines))
	}
}

func TestControlFrameBudgetPersistsAcrossTextMessages(t *testing.T) {
	// Interleaving text frames must not reset the control-frame limit.
	var stream []byte
	for i := 0; i < maxControlFramesPerWindow/2; i++ {
		stream = append(stream, maskedControlFrame(0xa, nil)...)
	}
	stream = append(stream, makeMaskedTextFrame(4)...)
	for i := 0; i < maxControlFramesPerWindow/2+1; i++ {
		stream = append(stream, maskedControlFrame(0xa, nil)...)
	}
	stream = append(stream, makeMaskedTextFrame(4)...)
	s := &socket{reader: bufio.NewReader(bytes.NewReader(stream))}
	if _, err := s.readText(); err != nil {
		t.Fatalf("valid control budget rejected: %v", err)
	}
	if _, err := s.readText(); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Fatalf("control flood unexpectedly accepted across text messages: %v", err)
	}
}

func TestInvalidControlFramesCannotRefreshIdleDeadline(t *testing.T) {
	// A non-final Ping frame is invalid under our nonfragmented protocol.
	bad := maskedControlFrame(0x9, []byte("wrong"))
	bad[0] &^= 0x80
	conn := &deadlineProbeConn{}
	s := &socket{Conn: conn, reader: bufio.NewReader(bytes.NewReader(bad))}
	called := false
	if _, err := s.readTextWithControl(func(byte, []byte) error {
		called = true
		return nil
	}); err == nil {
		t.Fatal("nonfinal control frame accepted")
	}
	if called {
		t.Fatal("invalid control frame refreshed deadline")
	}
}

func TestControlDeadlineUpdateFailureClosesRead(t *testing.T) {
	s := &socket{reader: bufio.NewReader(bytes.NewReader(maskedControlFrame(0xa, nil)))}
	expected := errors.New("deadline rejected")
	if _, err := s.readTextWithControl(func(byte, []byte) error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("deadline failure was ignored: %v", err)
	}
}
