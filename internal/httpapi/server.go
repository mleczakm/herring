// Package httpapi exposes Herring's HTTP endpoints.
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/mleczakm/herring/internal/storage/sqlite"
	"golang.org/x/crypto/bcrypt"
)

const maxSetupBody = 16 << 10

type setupStore interface {
	SetupRequired(context.Context) (bool, error)
	CreateInitialAdmin(context.Context, string, string, string) error
}

// Server serves health and first-run setup endpoints.
type Server struct {
	store      setupStore
	setupToken string
	logger     *slog.Logger
	template   *template.Template
}

// New creates the initial Herring HTTP handler.
func New(store setupStore, setupToken string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		store:      store,
		setupToken: setupToken,
		logger:     logger,
		template:   template.Must(template.New("setup").Parse(setupPage)),
	}
}

// Handler returns the complete HTTP router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /setup", s.showSetup)
	mux.HandleFunc("POST /setup", s.createAdmin)
	mux.HandleFunc("GET /", s.home)
	return securityHeaders(mux)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write([]byte("{\"status\":\"ok\"}\n"))
}

func (s *Server) home(response http.ResponseWriter, request *http.Request) {
	required, err := s.store.SetupRequired(request.Context())
	if err != nil {
		s.internalError(response, request, err)
		return
	}
	if required {
		http.Redirect(response, request, "/setup", http.StatusSeeOther)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = response.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Herring</title><body><main><h1>Herring</h1><p>Initial setup is complete.</p></main></body></html>`))
}

func (s *Server) showSetup(response http.ResponseWriter, request *http.Request) {
	required, err := s.store.SetupRequired(request.Context())
	if err != nil {
		s.internalError(response, request, err)
		return
	}
	if !required {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	s.renderSetup(response, http.StatusOK, setupView{TokenRequired: s.setupToken != ""})
}

func (s *Server) createAdmin(response http.ResponseWriter, request *http.Request) {
	if !sameOrigin(request) {
		http.Error(response, "invalid request origin", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxSetupBody)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid setup form", http.StatusBadRequest)
		return
	}

	required, err := s.store.SetupRequired(request.Context())
	if err != nil {
		s.internalError(response, request, err)
		return
	}
	if !required {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}

	view := setupView{
		DisplayName:   strings.TrimSpace(request.FormValue("display_name")),
		Email:         strings.TrimSpace(strings.ToLower(request.FormValue("email"))),
		TokenRequired: s.setupToken != "",
	}
	password := request.FormValue("password")
	if message := s.validateSetup(view, password, request.FormValue("password_confirmation"), request.FormValue("setup_token")); message != "" {
		view.Error = message
		s.renderSetup(response, http.StatusUnprocessableEntity, view)
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.internalError(response, request, errors.New("could not hash administrator password"))
		return
	}
	if err := s.store.CreateInitialAdmin(request.Context(), view.Email, view.DisplayName, string(passwordHash)); err != nil {
		if errors.Is(err, sqlite.ErrSetupComplete) {
			http.Redirect(response, request, "/", http.StatusSeeOther)
			return
		}
		s.internalError(response, request, err)
		return
	}

	s.logger.Info("initial administrator created", "email", view.Email)
	http.Redirect(response, request, "/?setup=complete", http.StatusSeeOther)
}

func (s *Server) validateSetup(view setupView, password, confirmation, providedToken string) string {
	if s.setupToken != "" && !secureEqual(providedToken, s.setupToken) {
		return "The setup token is invalid."
	}
	if length := utf8.RuneCountInString(view.DisplayName); length < 2 || length > 80 {
		return "Display name must contain between 2 and 80 characters."
	}
	address, err := mail.ParseAddress(view.Email)
	if err != nil || address.Address != view.Email || len(view.Email) > 254 {
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

func (s *Server) renderSetup(response http.ResponseWriter, status int, view setupView) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if err := s.template.Execute(response, view); err != nil {
		s.logger.Error("could not render setup page", "error", err)
	}
}

func (s *Server) internalError(response http.ResponseWriter, request *http.Request, err error) {
	s.logger.Error("HTTP request failed", "method", request.Method, "path", request.URL.Path, "error", err)
	http.Error(response, "internal server error", http.StatusInternalServerError)
}

type setupView struct {
	DisplayName   string
	Email         string
	Error         string
	TokenRequired bool
}

func secureEqual(provided, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, request.Host)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(response, request)
	})
}

const setupPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Set up Herring</title>
  <style>
    :root { color-scheme: light dark; font: 16px/1.5 system-ui, sans-serif; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #102326; }
    main { width: min(28rem, calc(100% - 2rem)); box-sizing: border-box; padding: 2rem; border-radius: 1rem; background: #f8f4e8; color: #172426; box-shadow: 0 1rem 3rem #0005; }
    h1 { margin-top: 0; color: #0c6064; }
    label { display: block; margin-top: 1rem; font-weight: 650; }
    input { width: 100%; box-sizing: border-box; margin-top: .35rem; padding: .7rem; border: 1px solid #87999a; border-radius: .45rem; background: white; color: #172426; }
    button { width: 100%; margin-top: 1.5rem; padding: .8rem; border: 0; border-radius: .45rem; background: #0c7479; color: white; font-weight: 700; cursor: pointer; }
    .error { padding: .75rem; border-radius: .45rem; background: #ffd9d5; color: #7d1710; }
    .hint { color: #526466; }
  </style>
</head>
<body><main>
  <h1>Welcome to Herring</h1>
  <p class="hint">Create the administrator account for this installation. This page closes after the first account is saved.</p>
  {{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
  <form method="post" action="/setup">
    {{if .TokenRequired}}<label>Setup token<input name="setup_token" type="password" required autocomplete="one-time-code"></label>{{end}}
    <label>Display name<input name="display_name" required maxlength="80" autocomplete="name" value="{{.DisplayName}}"></label>
    <label>Email<input name="email" type="email" required maxlength="254" autocomplete="email" value="{{.Email}}"></label>
    <label>Password<input name="password" type="password" required minlength="12" maxlength="72" autocomplete="new-password"></label>
    <label>Confirm password<input name="password_confirmation" type="password" required minlength="12" maxlength="72" autocomplete="new-password"></label>
    <button type="submit">Create administrator</button>
  </form>
</main></body></html>`
