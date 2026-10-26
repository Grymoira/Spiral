// Package api provides a loopback-only development HTTP interface to room membership.
// Internet-facing access must wait for authenticated WSS and TLS configuration.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Grymoira/Spiral/server/internal/rooms"
)

const maxRequestBytes = 4096

type Handler struct{ Store *rooms.Store }

func NewHandler(store *rooms.Store) http.Handler { return &Handler{Store: store} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		creds, err := h.Store.Create()
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
			InviteCode string `json:"inviteCode"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json")
			return
		}
		creds, err := h.Store.Join(roomID, body.InviteCode)
		if err != nil {
			writeRoomError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, creds)
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
	if len(parts) == 3 && parts[1] == "members" && parts[2] == "me" && r.Method == http.MethodDelete {
		if err := h.Store.Leave(roomID, bearer(r)); err != nil {
			writeRoomError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("content-type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
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
