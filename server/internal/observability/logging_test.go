package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONLogSchemaAndSecretsNeverLogged(t *testing.T) {
	var logs bytes.Buffer
	logger := NewLogger(&logs, slog.LevelInfo)
	wrapped := HTTPLogs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}), logger)
	secret := "secret-in-url-or-header"
	req := httptest.NewRequest(http.MethodGet, "/v1/rooms/"+secret+"?token="+secret, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	response := httptest.NewRecorder()
	wrapped.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden || response.Header().Get("X-Request-ID") == "" {
		t.Fatal("HTTP status/request ID not preserved")
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("credential leaked into JSON log: %s", logs.String())
	}
	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"timestamp", "level", "event", "schema_version", "service", "request_id", "component", "method", "route", "status", "duration_ms"} {
		if _, ok := record[key]; !ok {
			t.Fatalf("missing structured log key %s", key)
		}
	}
	if record["event"] != "http.request_completed" || record["route"] != "rooms.snapshot" || record["status"] != float64(403) {
		t.Fatalf("unexpected structured log: %v", record)
	}
}

func TestWebSocketWriterIsNotWrapped(t *testing.T) {
	var logs bytes.Buffer
	original := httptest.NewRecorder()
	handler := HTTPLogs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w != original {
			t.Error("websocket ResponseWriter was wrapped (hijack risk)")
		}
	}), NewLogger(&logs, slog.LevelInfo))
	handler.ServeHTTP(original, httptest.NewRequest("GET", "/v1/ws", nil))
	if logs.Len() != 0 {
		t.Fatal("unexpected WS HTTP log")
	}
}
