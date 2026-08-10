package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mleczakm/herring/internal/httpapi/assets"
	"github.com/mleczakm/herring/internal/storage/sqlite"
	"github.com/mleczakm/herring/internal/tracker/st901"
	"golang.org/x/crypto/bcrypt"
)

const maxBody = 16 << 10

type Sender interface {
	Send(context.Context, string, string) (string, error)
}
type Store interface {
	SetupRequired(context.Context) (bool, error)
	CreateInitialAdmin(context.Context, string, string, string) error
	UserByEmail(context.Context, string) (sqlite.User, error)
	CreateSession(context.Context, string, int64, time.Time) error
	SessionUser(context.Context, string, time.Time) (sqlite.User, error)
	DeleteSession(context.Context, string) error
	CreateManagedDevice(context.Context, string, string, string) (sqlite.ManagedDevice, error)
	ManagedDevices(context.Context) ([]sqlite.ManagedDevice, error)
	LatestPositions(context.Context) ([]sqlite.DevicePosition, error)
	ManagedDeviceByPhone(context.Context, string) (sqlite.ManagedDevice, error)
	RecordSMSCommand(context.Context, int64, string, string, string) error
	UpdateSMSDelivery(context.Context, string, string) error
	SetConfigurationStatus(context.Context, int64, string, string) error
}
type Config struct {
	SetupToken, WebhookSecret string
	PublicOrigin              string
	SecureCookies             bool
	Tracker                   st901.Profile
}
type Server struct {
	store  Store
	sender Sender
	config Config
	logger *slog.Logger
	pages  *template.Template
}

func New(store Store, sender Sender, config Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{store: store, sender: sender, config: config, logger: logger, pages: template.Must(template.New("pages").Parse(pages))}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /assets/leaflet.js", s.asset("text/javascript; charset=utf-8", assets.LeafletJS))
	mux.HandleFunc("GET /assets/leaflet.css", s.asset("text/css; charset=utf-8", assets.LeafletCSS))
	mux.HandleFunc("GET /assets/space-grotesk-latin.woff2", s.asset("font/woff2", assets.SpaceGroteskLatin))
	mux.HandleFunc("GET /assets/space-grotesk-latin-ext.woff2", s.asset("font/woff2", assets.SpaceGroteskLatinExt))
	mux.HandleFunc("GET /assets/dashboard.js", s.asset("text/javascript; charset=utf-8", []byte(dashboardJS)))
	mux.HandleFunc("GET /setup", s.showSetup)
	mux.HandleFunc("POST /setup", s.createAdmin)
	mux.HandleFunc("GET /login", s.showLogin)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("POST /devices", s.createDevice)
	mux.HandleFunc("GET /api/positions", s.positions)
	mux.HandleFunc("POST /webhooks/sendly/{secret}", s.webhook)
	return securityHeaders(mux)
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}
func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if s.redirectSetup(w, r) {
		return
	}
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, "home", 200, map[string]any{"User": user, "Devices": devices, "Ready": s.sender != nil})
}
func (s *Server) showLogin(w http.ResponseWriter, r *http.Request) {
	if s.redirectSetup(w, r) {
		return
	}
	s.render(w, "login", 200, nil)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_ = r.ParseForm()
	user, err := s.store.UserByEmail(r.Context(), r.FormValue("email"))
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(r.FormValue("password"))) != nil {
		s.render(w, "login", 422, map[string]any{"Error": "Nieprawidłowy email lub hasło."})
		return
	}
	token := randomToken()
	hash := tokenHash(token)
	if err := s.store.CreateSession(r.Context(), hash, user.ID, time.Now().Add(30*24*time.Hour)); err != nil {
		s.internalError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "herring_session", Value: token, Path: "/", HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	http.Redirect(w, r, "/", 303)
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return
	}
	if c, e := r.Cookie("herring_session"); e == nil {
		_ = s.store.DeleteSession(r.Context(), tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: "herring_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.config.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", 303)
}
func (s *Server) currentUser(r *http.Request) (sqlite.User, bool) {
	c, e := r.Cookie("herring_session")
	if e != nil {
		return sqlite.User{}, false
	}
	u, e := s.store.SessionUser(r.Context(), tokenHash(c.Value), time.Now())
	return u, e == nil
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return
	}
	if s.sender == nil {
		http.Error(w, "Sendly is not configured", 503)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_ = r.ParseForm()
	model := r.FormValue("model")
	if model != "st901-2g" && model != "st901-4g" {
		http.Error(w, "unsupported tracker model", 422)
		return
	}
	phone, err := st901.NormalizePhone(r.FormValue("phone"))
	if err != nil {
		http.Error(w, err.Error(), 422)
		return
	}
	device, err := s.store.CreateManagedDevice(r.Context(), r.FormValue("name"), model, phone)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	commands, err := s.config.Tracker.Commands()
	if err != nil {
		_ = s.store.SetConfigurationStatus(r.Context(), device.ID, "failed", "Niepełna konfiguracja serwera.")
		s.internalError(w, r, err)
		return
	}
	for _, command := range commands {
		id, sendErr := s.sender.Send(r.Context(), phone, command.Body)
		if sendErr != nil {
			_ = s.store.SetConfigurationStatus(r.Context(), device.ID, "failed", "Nie udało się wysłać komendy „"+command.Kind+"”.")
			s.internalError(w, r, sendErr)
			return
		}
		if err := s.store.RecordSMSCommand(r.Context(), device.ID, command.Kind, id, "accepted"); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	_ = s.store.SetConfigurationStatus(r.Context(), device.ID, "awaiting_reply", "SMS-y wysłane. Oczekiwanie na odpowiedź RCONF trackera.")
	http.Redirect(w, r, "/?device="+strconv.FormatInt(device.ID, 10), 303)
}
func (s *Server) asset(contentType string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(body)
	}
}
func (s *Server) positions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	positions, err := s.store.LatestPositions(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	type position struct {
		ID           int64   `json:"id"`
		Name         string  `json:"name"`
		Model        string  `json:"model"`
		ConfigStatus string  `json:"config_status"`
		ConfigDetail string  `json:"config_detail"`
		HasPosition  bool    `json:"has_position"`
		Latitude     float64 `json:"latitude,omitempty"`
		Longitude    float64 `json:"longitude,omitempty"`
		SpeedKPH     float64 `json:"speed_kph,omitempty"`
		Heading      float64 `json:"heading,omitempty"`
		GPSValid     bool    `json:"gps_valid,omitempty"`
		TrackerTime  string  `json:"tracker_time,omitempty"`
		ReceivedAt   string  `json:"received_at,omitempty"`
	}
	out := make([]position, 0, len(positions))
	for _, p := range positions {
		name := p.Device.Name
		if name == "" {
			name = p.Device.PhoneNumber
		}
		item := position{ID: p.Device.ID, Name: name, Model: p.Device.Model, ConfigStatus: p.Device.ConfigStatus, ConfigDetail: p.Device.ConfigDetail, HasPosition: p.HasPosition}
		if p.HasPosition {
			item.Latitude, item.Longitude, item.SpeedKPH, item.Heading = p.Latitude, p.Longitude, p.SpeedKPH, p.Heading
			item.GPSValid = p.GPSValid
			item.TrackerTime = p.TrackerTime.UTC().Format(time.RFC3339)
			item.ReceivedAt = p.ReceivedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if s.config.WebhookSecret == "" || !secureEqual(r.PathValue("secret"), s.config.WebhookSecret) {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var event struct {
		Type      string `json:"type"`
		From      string `json:"from"`
		To        string `json:"to"`
		Body      string `json:"body"`
		MessageID string `json:"message_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid event", 400)
		return
	}
	switch event.Type {
	case "NOTIFICATION":
		if event.MessageID == "" {
			http.Error(w, "missing message ID", 400)
			return
		}
		if err := s.store.UpdateSMSDelivery(r.Context(), event.MessageID, event.Status); err != nil {
			s.internalError(w, r, err)
			return
		}
	case "MESSAGE":
		phone, err := st901.NormalizePhone(event.From)
		if err != nil {
			http.Error(w, "invalid sender", 400)
			return
		}
		d, err := s.store.ManagedDeviceByPhone(r.Context(), phone)
		if err != nil {
			http.Error(w, "unknown sender", 404)
			return
		}
		if st901.ConfigurationMatches(event.Body, s.config.Tracker) {
			_ = s.store.SetConfigurationStatus(r.Context(), d.ID, "configured", "Tracker potwierdził APN, tryb GPRS i endpoint Herring.")
		} else if strings.Contains(strings.ToUpper(event.Body), "RCONF") || strings.Contains(strings.ToUpper(event.Body), "MODE:") {
			_ = s.store.SetConfigurationStatus(r.Context(), d.ID, "failed", "Tracker odpowiedział, ale ustawienia nie zgadzają się z profilem Herring.")
		}
	default:
		http.Error(w, "unsupported event", 400)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) redirectSetup(w http.ResponseWriter, r *http.Request) bool {
	required, err := s.store.SetupRequired(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return true
	}
	if required {
		http.Redirect(w, r, "/setup", 303)
		return true
	}
	return false
}
func (s *Server) showSetup(w http.ResponseWriter, r *http.Request) {
	required, err := s.store.SetupRequired(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !required {
		http.Redirect(w, r, "/login", 303)
		return
	}
	s.render(w, "setup", 200, map[string]any{"TokenRequired": s.config.SetupToken != ""})
}
func (s *Server) createAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid setup form", 400)
		return
	}
	required, err := s.store.SetupRequired(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !required {
		http.Redirect(w, r, "/login", 303)
		return
	}
	view := map[string]any{"DisplayName": strings.TrimSpace(r.FormValue("display_name")), "Email": strings.TrimSpace(strings.ToLower(r.FormValue("email"))), "TokenRequired": s.config.SetupToken != ""}
	password := r.FormValue("password")
	if msg := s.validateSetup(fmt.Sprint(view["DisplayName"]), fmt.Sprint(view["Email"]), password, r.FormValue("password_confirmation"), r.FormValue("setup_token")); msg != "" {
		view["Error"] = msg
		s.render(w, "setup", 422, view)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.store.CreateInitialAdmin(r.Context(), fmt.Sprint(view["Email"]), fmt.Sprint(view["DisplayName"]), string(hash)); err != nil {
		if errors.Is(err, sqlite.ErrSetupComplete) {
			http.Redirect(w, r, "/login", 303)
			return
		}
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/login?setup=complete", 303)
}
func (s *Server) validateSetup(name, email, password, confirmation, token string) string {
	if s.config.SetupToken != "" && !secureEqual(token, s.config.SetupToken) {
		return "The setup token is invalid."
	}
	if n := utf8.RuneCountInString(name); n < 2 || n > 80 {
		return "Display name must contain between 2 and 80 characters."
	}
	a, e := mail.ParseAddress(email)
	if e != nil || a.Address != email || len(email) > 254 {
		return "Enter a valid email address."
	}
	if len(password) < 12 {
		return "Password must contain at least 12 characters."
	}
	if len(password) > 72 {
		return "Password must not exceed 72 bytes."
	}
	if password != confirmation {
		return "Password confirmation does not match."
	}
	return ""
}
func (s *Server) render(w http.ResponseWriter, name string, status int, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.pages.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("render page", "error", err)
	}
}
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("HTTP request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	http.Error(w, "internal server error", 500)
}
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func secureEqual(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}
func (s *Server) sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return true
	}
	u, e := url.Parse(o)
	if e != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if s.config.PublicOrigin != "" {
		expected, err := url.Parse(s.config.PublicOrigin)
		return err == nil && strings.EqualFold(u.Scheme, expected.Scheme) && strings.EqualFold(u.Host, expected.Host)
	}
	return strings.EqualFold(u.Host, r.Host)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://*.basemaps.cartocdn.com; style-src 'self' 'unsafe-inline'; font-src 'self'; script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

var _ = sql.ErrNoRows
