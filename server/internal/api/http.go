// Package api provides a loopback-only development HTTP interface to room membership.
// Internet-facing access must wait for authenticated WSS and TLS configuration.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"reflect"
	"strings"

	"github.com/Grymoira/Spiral/server/internal/realtime"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

const maxRequestBytes = 4096

type Handler struct {
	Store *rooms.Store
	Hub   *realtime.Hub
}

func NewHandler(store *rooms.Store) http.Handler {
	return &Handler{Store: store, Hub: realtime.NewHub(store)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/ws" {
		h.Hub.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if r.URL.Path == "/v1/rooms" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		// Creating a room has no input fields in this baseline API.
		if err := decodeJSON(w, r, &struct{}{}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		// Use the peer address assigned by net/http, never user-provided
		// Forwarded headers (which are spoofable without a trusted proxy).
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_client_address")
			return
		}
		creds, err := h.Store.CreateForSource(host)
		if err != nil {
			writeRoomError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, creds)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v1/rooms/") {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/rooms/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	roomID := parts[0]
	if len(parts) == 2 && parts[1] == "join" && r.Method == http.MethodPost {
		var body struct {
			InviteCode *string `json:"inviteCode"`
		}
		if err := decodeJSON(w, r, &body); err != nil || body.InviteCode == nil || *body.InviteCode == "" {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		creds, err := h.Store.Join(roomID, *body.InviteCode)
		if err != nil {
			writeRoomError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, creds)
		h.Hub.Broadcast(roomID)
		return
	}
	if len(parts) == 2 && parts[1] == "join" {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		snap, err := h.Store.Snapshot(roomID, bearer(r))
		if err != nil {
			writeRoomError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snap)
		return
	}
	if len(parts) == 1 {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if len(parts) == 3 && parts[1] == "members" && parts[2] == "me" && r.Method == http.MethodDelete {
		if err := h.Store.Leave(roomID, bearer(r)); err != nil {
			writeRoomError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		h.Hub.Broadcast(roomID)
		return
	}
	if len(parts) == 3 && parts[1] == "members" && parts[2] == "me" {
		w.Header().Set("Allow", "DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	writeError(w, http.StatusNotFound, "not_found")
}

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

// decodeJSON accepts only a single JSON object with unique top-level keys.
// Keeping the wire contract strict avoids ambiguity between future clients.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		return errors.New("content-type must be application/json with UTF-8 encoding")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		return err
	}
	if err := validateObjectKeys(body, v); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v)
}

func validateObjectKeys(body []byte, v any) error {
	allowed := map[string]bool{}
	typ := reflect.TypeOf(v)
	if typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return errors.New("JSON schema must be a struct pointer")
	}
	typ = typ.Elem()
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" {
			tag = typ.Field(i).Name
		}
		if tag != "-" {
			allowed[tag] = true
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	if first != json.Delim('{') {
		return errors.New("JSON root must be an object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] || !allowed[key] {
			return errors.New("duplicate or invalid JSON object key")
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') {
		return errors.New("invalid JSON object end")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func writeRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rooms.ErrNotFound):
		writeError(w, http.StatusNotFound, "room_not_found")
	case errors.Is(err, rooms.ErrForbidden):
		writeError(w, http.StatusForbidden, "invalid_invite")
	case errors.Is(err, rooms.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, rooms.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "room_limit_reached")
	case errors.Is(err, rooms.ErrRateLimited):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "room_creation_rate_limited")
	case errors.Is(err, rooms.ErrSourceQuota):
		writeError(w, http.StatusTooManyRequests, "source_room_limit_reached")
	case errors.Is(err, rooms.ErrInvalidSource):
		writeError(w, http.StatusBadRequest, "invalid_client_address")
	case errors.Is(err, rooms.ErrFull):
		writeError(w, http.StatusConflict, "room_full")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

func writeError(w http.ResponseWriter, code int, value string) {
	writeJSON(w, code, map[string]string{"error": value})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
