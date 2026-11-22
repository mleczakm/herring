package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

type alertStore interface {
	Places(context.Context, int64) ([]sqlite.Place, error)
	CreatePlace(context.Context, int64, sqlite.Place) (sqlite.Place, error)
	DeletePlace(context.Context, int64, int64) (bool, error)
	PlaceSuggestions(context.Context, int64, time.Time) ([]sqlite.PlaceSuggestion, error)
	DeviceAlerts(context.Context, int64, int64) (sqlite.AlertSettings, error)
	SaveDeviceAlerts(context.Context, int64, int64, sqlite.AlertSettings) error
}

func (s *Server) alertRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/places", s.places)
	m.HandleFunc("POST /api/places", s.places)
	m.HandleFunc("DELETE /api/places/{id}", s.places)
	m.HandleFunc("GET /api/place-suggestions", s.placeSuggestions)
	m.HandleFunc("GET /api/devices/{id}/alerts", s.deviceAlerts)
	m.HandleFunc("PUT /api/devices/{id}/alerts", s.deviceAlerts)
}
func (s *Server) alertUser(w http.ResponseWriter, r *http.Request) (sqlite.User, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		jsonError(w, http.StatusUnauthorized, "unauthorized")
	}
	return user, ok
}

func alertJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) places(w http.ResponseWriter, r *http.Request) {
	u, ok := s.alertUser(w, r)
	if !ok {
		return
	}
	if r.Method != "GET" && !s.jsonContentType(w, r) {
		return
	}
	switch r.Method {
	case "GET":
		p, err := s.store.Places(r.Context(), u.ID)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		alertJSON(w, p)
	case "POST":
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		var p sqlite.Place
		if json.NewDecoder(r.Body).Decode(&p) != nil {
			jsonError(w, 400, "invalid_place")
			return
		}
		p, err := s.store.CreatePlace(r.Context(), u.ID, p)
		if err != nil {
			jsonError(w, 400, "invalid_place")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(p)
	case "DELETE":
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			jsonError(w, 400, "invalid_id")
			return
		}
		found, err := s.store.DeletePlace(r.Context(), u.ID, id)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !found {
			jsonError(w, 404, "not_found")
			return
		}
		w.WriteHeader(204)
	}
}
func (s *Server) placeSuggestions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.alertUser(w, r)
	if !ok {
		return
	}
	p, err := s.store.PlaceSuggestions(r.Context(), u.ID, time.Now())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	alertJSON(w, p)
}
func (s *Server) deviceAlerts(w http.ResponseWriter, r *http.Request) {
	u, ok := s.alertUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		jsonError(w, 400, "invalid_id")
		return
	}
	if r.Method == "GET" {
		a, err := s.store.DeviceAlerts(r.Context(), u.ID, id)
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, 404, "not_found")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		alertJSON(w, a)
		return
	}
	if !s.jsonContentType(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var a sqlite.AlertSettings
	if json.NewDecoder(r.Body).Decode(&a) != nil {
		jsonError(w, 400, "invalid_settings")
		return
	}
	err = s.store.SaveDeviceAlerts(r.Context(), u.ID, id, a)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, 404, "not_found")
		return
	}
	if err != nil {
		jsonError(w, 400, "invalid_settings")
		return
	}
	w.WriteHeader(204)
}
