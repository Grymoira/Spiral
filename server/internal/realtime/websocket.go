// Package realtime provides a small, bounded RFC 6455 text-message transport.
// It intentionally avoids third-party dependencies and does not implement compression.
package realtime

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxMessage = 4096

var errClosed = errors.New("websocket closed")

type socket struct {
	net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

func headerHas(value, expected string) bool {
	for _, s := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(s), expected) {
			return true
		}
	}
	return false
}

func upgrade(w http.ResponseWriter, r *http.Request) (*socket, error) {
	if r.Method != http.MethodGet || !headerHas(r.Header.Get("Connection"), "upgrade") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("invalid websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 {
		return nil, errors.New("invalid websocket key")
	}
	// Native clients need not send Origin. Browser cross-site sockets are rejected.
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || !strings.EqualFold(parsed.Host, r.Host) {
			return nil, errors.New("cross-origin websocket rejected")
		}
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("websocket hijack unavailable")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, err = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(digest[:]))
	if err == nil {
		err = rw.Flush()
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &socket{Conn: conn, reader: rw.Reader}, nil
}

func (s *socket) writeFrame(opcode byte, message []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	var header []byte
	if len(message) < 126 {
		header = []byte{0x80 | opcode, byte(len(message))}
	} else if len(message) <= 65535 {
		header = []byte{0x80 | opcode, 126, byte(len(message) >> 8), byte(len(message))}
	} else {
		return errors.New("websocket output too large")
	}
	if _, err := s.Conn.Write(header); err != nil {
		return err
	}
	_, err := s.Conn.Write(message)
	return err
}

func (s *socket) readText() ([]byte, error) {
	for {
		var h [2]byte
		if _, err := io.ReadFull(s.reader, h[:]); err != nil {
			return nil, err
		}
		if h[0]&0x70 != 0 || h[0]&0x80 == 0 || h[1]&0x80 == 0 {
			return nil, errors.New("invalid websocket flags or missing client mask")
		}
		op := h[0] & 0x0f
		if op != 0x1 && op != 0x8 && op != 0x9 && op != 0xa {
			return nil, errors.New("unsupported websocket opcode")
		}
		size := uint64(h[1] & 0x7f)
		if size == 126 {
			var b [2]byte
			if _, err := io.ReadFull(s.reader, b[:]); err != nil {
				return nil, err
			}
			size = uint64(binary.BigEndian.Uint16(b[:]))
		}
		if size == 127 {
			var b [8]byte
			if _, err := io.ReadFull(s.reader, b[:]); err != nil {
				return nil, err
			}
			size = binary.BigEndian.Uint64(b[:])
		}
		if size > maxMessage || ((op == 0x8 || op == 0x9 || op == 0xa) && size > 125) {
			return nil, errors.New("websocket message too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(s.reader, mask[:]); err != nil {
			return nil, err
		}
		data := make([]byte, int(size))
		if _, err := io.ReadFull(s.reader, data); err != nil {
			return nil, err
		}
		for i := range data {
			data[i] ^= mask[i%4]
		}
		switch op {
		case 0x8:
			_ = s.writeFrame(0x8, []byte{})
			return nil, errClosed
		case 0x9:
			if err := s.writeFrame(0xa, data); err != nil {
				return nil, err
			}
			continue
		case 0xa:
			continue
		case 0x1:
			if !utf8.Valid(data) {
				return nil, errors.New("invalid UTF-8 frame")
			}
			return data, nil
		}
	}
}
