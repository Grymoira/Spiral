package realtime

import (
	"bufio"
	"bytes"
	"fmt"
	"testing"
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
