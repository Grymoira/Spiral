package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func publicCreate(h http.Handler, address, forwarded string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/rooms", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = address
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
		req.Header.Set("Forwarded", "for="+forwarded)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPublicRoomCreateRateAndSourceIdentity(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	for i := 0; i < 6; i++ {
		// Spoofed forwarded headers MUST NOT create independent identities.
		got := publicCreate(h, "192.0.2.22:4567", fmt.Sprintf("203.0.113.%d", i+1))
		if got.Code != http.StatusCreated {
			t.Fatalf("create %d: %d: %s", i, got.Code, got.Body.String())
		}
		var creds rooms.Credentials
		if err := json.NewDecoder(got.Body).Decode(&creds); err != nil || creds.SessionToken == "" {
			t.Fatal("room credentials missing from admitted request")
		}
	}
	rejected := publicCreate(h, "192.0.2.22:4567", "203.0.113.100")
	if rejected.Code != http.StatusTooManyRequests || rejected.Header().Get("Retry-After") != "60" ||
		!strings.Contains(rejected.Body.String(), "room_creation_rate_limited") {
		t.Fatalf("unexpected rate response: %d headers=%v body=%s", rejected.Code, rejected.Header(), rejected.Body.String())
	}
	if other := publicCreate(h, "192.0.2.23:4567", "192.0.2.22"); other.Code != http.StatusCreated {
		t.Fatalf("second peer lost fair access: %d", other.Code)
	}
	if invalid := publicCreate(h, "invalid-address", "203.0.113.1"); invalid.Code != http.StatusBadRequest {
		t.Fatalf("malformed TCP peer was accepted: %d", invalid.Code)
	}
}

func TestPublicCreateConcurrentHTTPBudget(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	var wg sync.WaitGroup
	status := make(chan int, 80)
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status <- publicCreate(h, "198.51.100.5:9999", "").Code
		}()
	}
	wg.Wait()
	close(status)
	accepted, rejected := 0, 0
	for code := range status {
		switch code {
		case http.StatusCreated:
			accepted++
		case http.StatusTooManyRequests:
			rejected++
		default:
			t.Fatalf("unexpected HTTP status: %d", code)
		}
	}
	if accepted != 6 || rejected != 74 {
		t.Fatalf("concurrent HTTP admissions exceeded budget: accepted=%d rejected=%d", accepted, rejected)
	}
}
