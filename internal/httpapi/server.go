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
	ManagedDeviceByID(context.Context, int64) (sqlite.ManagedDevice, error)
	ManagedDeviceByPhone(context.Context, string) (sqlite.ManagedDevice, error)
	RecordSMSCommand(context.Context, int64, string, string, string) error
	UpdateSMSDelivery(context.Context, string, string) error
	SetConfigurationStatus(context.Context, int64, string, string) error
}
type Config struct {
	SetupToken, WebhookSecret string
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
	mux.HandleFunc("GET /status.js", s.statusJS)
	mux.HandleFunc("GET /setup", s.showSetup)
	mux.HandleFunc("POST /setup", s.createAdmin)
	mux.HandleFunc("GET /login", s.showLogin)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("POST /devices", s.createDevice)
	mux.HandleFunc("GET /api/devices/{id}/configuration", s.configuration)
	mux.HandleFunc("POST /webhooks/sendly/{secret}", s.webhook)
	return securityHeaders(mux)
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
}
func (s *Server) statusJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(`document.querySelectorAll('[data-device]').forEach(function(el){var id=el.dataset.device;var timer=setInterval(function(){fetch('/api/devices/'+id+'/configuration').then(function(r){return r.json()}).then(function(v){var p=el.querySelector('.status');p.className='status '+v.status;p.textContent=(v.configured?'✓ Skonfigurowany':v.status==='failed'?'⚠ Konfiguracja nieudana':'⏳ Konfigurowanie…')+' — '+v.detail;if(v.configured||v.status==='failed')clearInterval(timer)}).catch(function(){})},2000)});`))
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
	if !sameOrigin(r) {
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
	if !sameOrigin(r) {
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
	if !sameOrigin(r) {
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
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		http.Error(w, "unauthorized", 401)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid device", 400)
		return
	}
	d, err := s.store.ManagedDeviceByID(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": d.ConfigStatus, "detail": d.ConfigDetail, "configured": d.ConfigStatus == "configured"})
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
	if !sameOrigin(r) {
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
func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, e := url.Parse(o)
	return e == nil && strings.EqualFold(u.Host, r.Host)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

var _ = sql.ErrNoRows

const pages = `{{define "style"}}<style>:root{font:16px/1.5 system-ui;color:#172426}body{margin:0;background:#102326}main{width:min(56rem,calc(100% - 2rem));margin:3rem auto;padding:2rem;box-sizing:border-box;border-radius:1rem;background:#f8f4e8}h1,h2{color:#0c6064}label{display:block;margin-top:1rem;font-weight:650}input,select{box-sizing:border-box;width:100%;padding:.7rem;margin-top:.3rem}button{margin-top:1rem;padding:.7rem 1rem;background:#0c7479;color:white;border:0;border-radius:.4rem}.error,.failed{color:#8b1710}.configured{color:#17622a}.device{padding:1rem;margin:1rem 0;background:white;border-radius:.6rem}.hint{color:#526466}nav{display:flex;justify-content:space-between}</style>{{end}}
{{define "setup"}}<!doctype html><html lang="pl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Herring — konfiguracja</title>{{template "style"}}<body><main><h1>Witaj w Herring</h1>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post">{{if .TokenRequired}}<label>Token instalacji<input name="setup_token" type="password" required></label>{{end}}<label>Nazwa<input name="display_name" value="{{.DisplayName}}" required></label><label>Email<input name="email" type="email" value="{{.Email}}" required></label><label>Hasło<input name="password" type="password" minlength="12" required></label><label>Powtórz hasło<input name="password_confirmation" type="password" minlength="12" required></label><button>Utwórz administratora</button></form></main></body></html>{{end}}
{{define "login"}}<!doctype html><html lang="pl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Herring — logowanie</title>{{template "style"}}<body><main><h1>Herring</h1>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post"><label>Email<input name="email" type="email" required></label><label>Hasło<input name="password" type="password" required></label><button>Zaloguj</button></form></main></body></html>{{end}}
{{define "home"}}<!doctype html><html lang="pl"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Herring</title>{{template "style"}}<body><main><nav><strong>Herring</strong><form method="post" action="/logout"><button>Wyloguj</button></form></nav><h1>Trackery</h1>{{range .Devices}}<section class="device" data-device="{{.ID}}"><strong>{{if .Name}}{{.Name}}{{else}}{{.PhoneNumber}}{{end}}</strong> · {{.Model}}<p class="status {{.ConfigStatus}}">{{if eq .ConfigStatus "configured"}}✓ Skonfigurowany{{else if eq .ConfigStatus "failed"}}⚠ Konfiguracja nieudana{{else}}⏳ Konfigurowanie…{{end}} — {{.ConfigDetail}}</p></section>{{else}}<p class="hint">Nie dodano jeszcze trackera.</p>{{end}}<h2>Dodaj tracker</h2>{{if .Ready}}<form method="post" action="/devices"><label>Wariant<select name="model"><option value="st901-2g">ST-901 2G</option><option value="st901-4g">ST-901 4G</option></select></label><label>Numer SIM trackera<input name="phone" type="tel" placeholder="+48…" required></label><label>Nazwa (opcjonalnie)<input name="name"></label><button>Dodaj i skonfiguruj przez SMS</button></form>{{else}}<p class="error">Administrator serwera musi najpierw skonfigurować integrację Sendly.</p>{{end}}<script src="/status.js"></script></main></body></html>{{end}}`
