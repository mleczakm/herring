package httpapi

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

// historyJS draws the "Historia tras" view. It reads a route either from
// GET /api/history or, in privacy mode, from the bound browser's IndexedDB
// (HerringPrivacy.route), and renders both through the same code.
//
//go:embed assets/history.js
var historyJS []byte

const (
	// historyPage bounds one API response; the browser asks for the next
	// page with the returned cursor.
	historyPage = 2000
	// historyMaxSpan bounds the range of one route query: 31 local
	// calendar days, plus slack for daylight saving and time zones.
	historyMaxSpan = 32 * 24 * time.Hour
)

type historyStore interface {
	RoutePositions(ctx context.Context, trackerID string, from, to time.Time, cursor *sqlite.RouteCursor, limit int) ([]sqlite.StoredPosition, error)
	OldestRouteTime(ctx context.Context, trackerID string) (time.Time, bool, error)
}

func (s *Server) showHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, "history", http.StatusOK, map[string]any{"Devices": devices, "Active": "history", "Privacy": settings.PrivacyMode, "RetentionDays": settings.ServerRetentionDays})
}

type historyPosition struct {
	ID          int64   `json:"id"`
	TrackerTime string  `json:"tracker_time"`
	ReceivedAt  string  `json:"received_at"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	SpeedKPH    float64 `json:"speed_kph"`
	Heading     float64 `json:"heading"`
	Source      string  `json:"source"`
}

// history returns one page of a device's route between from and to
// (RFC 3339, at most historyMaxSpan apart). In privacy mode the server keeps
// no history to show, so it answers 409 and the bound browser reads its own.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		jsonError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if settings.PrivacyMode {
		jsonError(w, http.StatusConflict, "privacy_mode")
		return
	}
	q := r.URL.Query()
	deviceID, err := strconv.ParseInt(q.Get("device"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid_device")
		return
	}
	from, errFrom := time.Parse(time.RFC3339, q.Get("from"))
	to, errTo := time.Parse(time.RFC3339, q.Get("to"))
	if errFrom != nil || errTo != nil || !to.After(from) || to.Sub(from) > historyMaxSpan {
		jsonError(w, http.StatusBadRequest, "invalid_range")
		return
	}
	var cursor *sqlite.RouteCursor
	if raw := q.Get("cursor"); raw != "" {
		c, ok := parseRouteCursor(raw)
		if !ok {
			jsonError(w, http.StatusBadRequest, "invalid_cursor")
			return
		}
		cursor = &c
	}
	device, err := s.store.ManagedDevice(r.Context(), deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "unknown_device")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	response := struct {
		Positions     []historyPosition `json:"positions"`
		More          bool              `json:"more"`
		Cursor        string            `json:"cursor,omitempty"`
		Oldest        string            `json:"oldest,omitempty"`
		RetentionDays int               `json:"retention_days"`
	}{Positions: []historyPosition{}, RetentionDays: settings.ServerRetentionDays}
	if device.TrackerID != "" {
		positions, err := s.store.RoutePositions(r.Context(), device.TrackerID, from, to, cursor, historyPage+1)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if len(positions) > historyPage {
			positions = positions[:historyPage]
			last := positions[len(positions)-1]
			response.More = true
			response.Cursor = last.TrackerTime.UTC().Format(time.RFC3339Nano) + "," + strconv.FormatInt(last.ID, 10)
		}
		for _, p := range positions {
			response.Positions = append(response.Positions, historyPosition{
				ID: p.ID, TrackerTime: p.TrackerTime.UTC().Format(time.RFC3339Nano), ReceivedAt: p.ReceivedAt.UTC().Format(time.RFC3339Nano),
				Latitude: p.Latitude, Longitude: p.Longitude, SpeedKPH: p.SpeedKPH, Heading: p.Heading, Source: p.Source,
			})
		}
		if cursor == nil {
			oldest, ok, err := s.store.OldestRouteTime(r.Context(), device.TrackerID)
			if err != nil {
				s.internalError(w, r, err)
				return
			}
			if ok {
				response.Oldest = oldest.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}

func parseRouteCursor(raw string) (sqlite.RouteCursor, bool) {
	at, id, ok := strings.Cut(raw, ",")
	if !ok {
		return sqlite.RouteCursor{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return sqlite.RouteCursor{}, false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n < 0 {
		return sqlite.RouteCursor{}, false
	}
	return sqlite.RouteCursor{TrackerTime: t, ID: n}, true
}
