package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func request(t *testing.T, h http.Handler, method, path, data, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(data))
	if data != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHTTPRoomLifecycle(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	if w := request(t, h, "GET", "/healthz", "", ""); w.Code != 200 {
		t.Fatalf("health: %d", w.Code)
	}
	w := request(t, h, "POST", "/v1/rooms", "{}", "")
	if w.Code != 201 {
		t.Fatalf("create: %d: %s", w.Code, w.Body)
	}
	var host rooms.Credentials
	if err := json.NewDecoder(w.Body).Decode(&host); err != nil {
		t.Fatal(err)
	}
	roomPath := "/v1/rooms/" + host.RoomID
	if w = request(t, h, "GET", roomPath, "", ""); w.Code != 401 {
		t.Fatalf("no bearer: %d", w.Code)
	}
	if w = request(t, h, "GET", roomPath, "", host.SessionToken); w.Code != 200 {
		t.Fatalf("host snapshot: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"inviteCode": host.InviteCode})
	w = request(t, h, "POST", roomPath+"/join", string(body), "")
	if w.Code != 201 {
		t.Fatalf("join: %d: %s", w.Code, w.Body)
	}
	var guest rooms.Credentials
	if err := json.NewDecoder(w.Body).Decode(&guest); err != nil {
		t.Fatal(err)
	}
	if guest.InviteCode != "" {
		t.Fatal("invite leaked")
	}
	if w = request(t, h, "DELETE", roomPath+"/members/me", "", guest.SessionToken); w.Code != 204 {
		t.Fatalf("leave: %d", w.Code)
	}
}

func TestStrictJSONAndMethod(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	for _, body := range []string{`{"unknown":1}`, `{} {}`, `{"a":1}`, strings.Repeat(" ", 5000) + "{}"} {
		w := request(t, h, "POST", "/v1/rooms", body, "")
		if w.Code != 400 {
			t.Errorf("body of %d bytes should fail: %d", len(body), w.Code)
		}
	}
	w := request(t, h, "PUT", "/v1/rooms", "{}", "")
	if w.Code != 405 {
		t.Fatalf("method: %d", w.Code)
	}
	req := httptest.NewRequest("POST", "/v1/rooms", bytes.NewBufferString("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("missing content-type: %d", rec.Code)
	}
}
