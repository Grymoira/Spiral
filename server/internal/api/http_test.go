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

func TestJSONRequestContract(t *testing.T) {
	handler := NewHandler(rooms.NewStore())
	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"empty", "/v1/rooms", "", 400},
		{"null", "/v1/rooms", "null", 400},
		{"array", "/v1/rooms", "[]", 400},
		{"scalar", "/v1/rooms", "12", 400},
		{"duplicateUnknown", "/v1/rooms", `{"x":1,"x":2}`, 400},
		{"trailingRoot", "/v1/rooms", `{} null`, 400},
		{"tooLarge", "/v1/rooms", `{` + strings.Repeat(" ", 4096) + `}`, 400},
		{"whitespaceOK", "/v1/rooms", " \n{} \n", 201},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}

func TestJoinRequiresUnambiguousInvite(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	w := request(t, h, "POST", "/v1/rooms", "{}", "")
	var host rooms.Credentials
	if err := json.NewDecoder(w.Body).Decode(&host); err != nil {
		t.Fatal(err)
	}
	path := "/v1/rooms/" + host.RoomID + "/join"
	for _, body := range []string{
		`{}`, `{"inviteCode":null}`, `{"inviteCode":""}`,
		`{"inviteCode":"bad","inviteCode":"good"}`, `{"inviteCode":42}`,
	} {
		if w := request(t, h, "POST", path, body, ""); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: want 400, got %d", body, w.Code)
		}
	}
	if w := request(t, h, "POST", path, `{"inviteCode":"wrong"}`, ""); w.Code != http.StatusForbidden {
		t.Errorf("wrong but well-formed invitation: want 403, got %d", w.Code)
	}
	payload, _ := json.Marshal(map[string]string{"inviteCode": host.InviteCode})
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid charset: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestKnownRoutesRejectWrongMethods(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	created := request(t, h, http.MethodPost, "/v1/rooms", "{}", "")
	var host rooms.Credentials
	if err := json.NewDecoder(created.Body).Decode(&host); err != nil {
		t.Fatal(err)
	}
	room := "/v1/rooms/" + host.RoomID
	for _, tc := range []struct{ method, path, allowed string }{
		{"GET", room + "/join", "POST"},
		{"PUT", room, "GET"},
		{"POST", room + "/members/me", "DELETE"},
	} {
		w := request(t, h, tc.method, tc.path, "", "")
		if w.Code != 405 || w.Header().Get("Allow") != tc.allowed {
			t.Errorf("%s %s: status %d, Allow %q", tc.method, tc.path, w.Code, w.Header().Get("Allow"))
		}
	}
}

func TestJSONSecurityHeaders(t *testing.T) {
	h := NewHandler(rooms.NewStore())
	for _, path := range []string{"/healthz", "/missing"} {
		rec := request(t, h, "GET", path, "", "")
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("security headers missing from %s", path)
		}
	}
}
