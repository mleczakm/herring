package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

const (
	// privacyBatch bounds one transfer to the privacy-mode browser.
	privacyBatch = 500
	// maxAckBody fits one acknowledged batch of position keys.
	maxAckBody = 128 << 10
	// clientHeader carries the secret that identifies the bound browser.
	clientHeader = "X-Herring-Client"
)

// Privacy mode moves tracked positions off the server. The server keeps each
// position only until the single bound browser has stored it locally and
// acknowledged it; other sessions never see positions while it is enabled.

func (s *Server) showSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	s.renderSettings(w, r, user, 200, r.URL.Query().Get("saved") == "1", "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, user sqlite.User, status int, saved bool, message string) {
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	count, err := s.store.CountPositions(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var boundAt string
	if !settings.PrivacyBoundAt.IsZero() {
		boundAt = settings.PrivacyBoundAt.UTC().Format(time.RFC3339)
	}
	s.render(w, "settings", status, map[string]any{
		"User": user, "Active": "settings", "Privacy": settings.PrivacyMode, "Settings": settings,
		"BoundAt": boundAt, "ServerPositions": count, "Saved": saved, "Error": message,
		"MaxRetentionDays": sqlite.MaxRetentionDays,
	})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_ = r.ParseForm()
	serverDays, serverErr := retentionDays(r.FormValue("server_retention_days"))
	browserDays, browserErr := retentionDays(r.FormValue("browser_retention_days"))
	if serverErr != nil || browserErr != nil {
		s.renderSettings(w, r, user, 422, false, "Okres przechowywania musi być liczbą dni od 0 do "+strconv.Itoa(sqlite.MaxRetentionDays)+".")
		return
	}
	if err := s.store.SaveRetention(r.Context(), serverDays, browserDays); err != nil {
		s.internalError(w, r, err)
		return
	}
	// Apply immediately so a shorter period takes effect without waiting for
	// the hourly retention run.
	if _, err := s.store.ApplyServerRetention(r.Context(), time.Now()); err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/ustawienia?saved=1", 303)
}

func retentionDays(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 || days > sqlite.MaxRetentionDays {
		return 0, strconv.ErrRange
	}
	return days, nil
}

func (s *Server) enablePrivacy(w http.ResponseWriter, r *http.Request) {
	if !s.jsonWrite(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var request struct {
		ClientKey string `json:"client_key"`
		Label     string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !validClientKey(request.ClientKey) {
		jsonError(w, 400, "invalid_client_key")
		return
	}
	if err := s.store.EnablePrivacy(r.Context(), tokenHash(request.ClientKey), cleanLabel(request.Label), time.Now()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.logger.Info("privacy mode bound to a browser")
	w.WriteHeader(204)
}

func (s *Server) disablePrivacy(w http.ResponseWriter, r *http.Request) {
	if !s.jsonWrite(w, r) {
		return
	}
	if err := s.store.DisablePrivacy(r.Context()); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.logger.Info("privacy mode disabled")
	w.WriteHeader(204)
}

type privacyDevice struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	ConfigStatus string `json:"config_status"`
	ConfigDetail string `json:"config_detail"`
	TrackerID    string `json:"tracker_id"`
}

type privacyPosition struct {
	ID          int64   `json:"id"`
	DeviceID    string  `json:"device_id"`
	TrackerTime string  `json:"tracker_time"`
	ReceivedAt  string  `json:"received_at"`
	GPSValid    bool    `json:"gps_valid"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	SpeedKPH    float64 `json:"speed_kph"`
	Heading     float64 `json:"heading"`
}

// privacySync hands the bound browser the oldest positions still on the
// server. They are deleted only after the browser acknowledges them.
func (s *Server) privacySync(w http.ResponseWriter, r *http.Request) {
	settings, ok := s.boundClient(w, r)
	if !ok {
		return
	}
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	positions, err := s.store.OldestPositions(r.Context(), privacyBatch+1)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	more := len(positions) > privacyBatch
	if more {
		positions = positions[:privacyBatch]
	}
	response := struct {
		Devices              []privacyDevice   `json:"devices"`
		Positions            []privacyPosition `json:"positions"`
		More                 bool              `json:"more"`
		BrowserRetentionDays int               `json:"browser_retention_days"`
	}{Devices: []privacyDevice{}, Positions: []privacyPosition{}, More: more, BrowserRetentionDays: settings.BrowserRetentionDays}
	for _, d := range devices {
		name := d.Name
		if name == "" {
			name = d.PhoneNumber
		}
		response.Devices = append(response.Devices, privacyDevice{ID: d.ID, Name: name, Model: d.Model, ConfigStatus: d.ConfigStatus, ConfigDetail: d.ConfigDetail, TrackerID: d.TrackerID})
	}
	for _, p := range positions {
		response.Positions = append(response.Positions, privacyPosition{
			ID: p.ID, DeviceID: p.DeviceID, TrackerTime: p.TrackerTime.UTC().Format(time.RFC3339Nano), ReceivedAt: p.ReceivedAt.UTC().Format(time.RFC3339Nano),
			GPSValid: p.GPSValid, Latitude: p.Latitude, Longitude: p.Longitude, SpeedKPH: p.SpeedKPH, Heading: p.Heading,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(response)
}

// privacyAck deletes positions the bound browser has stored durably.
func (s *Server) privacyAck(w http.ResponseWriter, r *http.Request) {
	if !s.jsonContentType(w, r) {
		return
	}
	if _, ok := s.boundClient(w, r); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAckBody)
	var request struct {
		Positions []struct {
			ID          int64  `json:"id"`
			DeviceID    string `json:"device_id"`
			TrackerTime string `json:"tracker_time"`
		} `json:"positions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Positions) > privacyBatch {
		jsonError(w, 400, "invalid_acknowledgement")
		return
	}
	keys := make([]sqlite.PositionKey, 0, len(request.Positions))
	for _, p := range request.Positions {
		trackerTime, err := time.Parse(time.RFC3339Nano, p.TrackerTime)
		if err != nil || p.DeviceID == "" {
			jsonError(w, 400, "invalid_acknowledgement")
			return
		}
		keys = append(keys, sqlite.PositionKey{ID: p.ID, DeviceID: p.DeviceID, TrackerTime: trackerTime})
	}
	deleted, err := s.store.DeletePositions(r.Context(), keys)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{"deleted": deleted})
}

// boundClient authorizes a request from the privacy-mode browser: it needs a
// session and the browser secret whose hash was stored when binding.
func (s *Server) boundClient(w http.ResponseWriter, r *http.Request) (sqlite.Settings, bool) {
	if _, ok := s.currentUser(r); !ok {
		jsonError(w, 401, "unauthorized")
		return sqlite.Settings{}, false
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return sqlite.Settings{}, false
	}
	if !settings.PrivacyMode {
		jsonError(w, 409, "privacy_disabled")
		return sqlite.Settings{}, false
	}
	key := r.Header.Get(clientHeader)
	if !validClientKey(key) || !secureEqual(tokenHash(key), settings.PrivacyClientHash) {
		jsonError(w, 403, "not_bound")
		return sqlite.Settings{}, false
	}
	return settings, true
}

// jsonWrite guards state-changing JSON endpoints called from the dashboard.
func (s *Server) jsonWrite(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := s.currentUser(r); !ok {
		jsonError(w, 401, "unauthorized")
		return false
	}
	return s.jsonContentType(w, r)
}

// jsonContentType rejects cross-origin and form-encoded requests. Requiring a
// JSON body means a cross-site HTML form cannot reach these endpoints.
func (s *Server) jsonContentType(w http.ResponseWriter, r *http.Request) bool {
	if !s.sameOrigin(r) {
		jsonError(w, 403, "invalid_origin")
		return false
	}
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		jsonError(w, 415, "json_required")
		return false
	}
	return true
}

func validClientKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

// cleanLabel keeps the browser description short and printable.
func cleanLabel(label string) string {
	label = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(label))
	for utf8.RuneCountInString(label) > 80 {
		_, size := utf8.DecodeLastRuneInString(label)
		label = label[:len(label)-size]
	}
	if label == "" {
		return "Nieznana przeglądarka"
	}
	return label
}

func jsonError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
