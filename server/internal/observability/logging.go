// Package observability defines Spiral's stable, privacy-preserving JSONL logs.
// Credentials, invite codes, URLs, media paths and user payloads are never logged.
package observability

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const SchemaVersion = 1

var nextRequestID atomic.Uint64

// NewLogger emits one JSON object per event with timestamp, level, event,
// schema_version and service. All timestamps are UTC RFC3339Nano.
func NewLogger(w io.Writer, level slog.Leveler) *slog.Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 {
				switch attr.Key {
				case slog.TimeKey:
					attr.Key = "timestamp"
					attr.Value = slog.TimeValue(attr.Value.Time().UTC())
				case slog.MessageKey:
					attr.Key = "event"
				}
			}
			return attr
		},
	})
	return slog.New(handler).With("service", "spiral-server", "schema_version", SchemaVersion)
}

// HTTPLogs records REST responses, not raw paths, bodies or request headers.
// WebSocket handshakes are passed through without wrapping ResponseWriter,
// retaining http.Hijacker support; WebSocket events are logged by the Hub.
func HTTPLogs(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ws" {
			next.ServeHTTP(w, r)
			return
		}
		id := fmt.Sprintf("http-%016x", nextRequestID.Add(1))
		w.Header().Set("X-Request-ID", id)
		start := time.Now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		logger.Log(r.Context(), level, "http.request_completed",
			"component", "http", "request_id", id,
			"method", r.Method, "route", routeName(r.URL.Path),
			"status", status, "duration_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

// routeName is a fixed allowlist: no untrusted URL segment is emitted.
func routeName(path string) string {
	if path == "/healthz" {
		return "health"
	}
	if path == "/v1/rooms" {
		return "rooms.create"
	}
	if !strings.HasPrefix(path, "/v1/rooms/") {
		return "unknown"
	}
	parts := strings.Split(strings.TrimPrefix(path, "/v1/rooms/"), "/")
	if len(parts) == 1 && parts[0] != "" {
		return "rooms.snapshot"
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "join" {
		return "rooms.join"
	}
	if len(parts) == 3 && parts[0] != "" && parts[1] == "members" && parts[2] == "me" {
		return "rooms.leave"
	}
	return "unknown"
}
