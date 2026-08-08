package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

func testServer(t *testing.T, token string) (*Server, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(store, nil, Config{SetupToken: token}, slog.New(slog.NewTextHandler(io.Discard, nil))), store
}
func TestHomeRedirectsToFirstRunSetup(t *testing.T) {
	server, _ := testServer(t, "")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/setup" {
		t.Fatalf("response %d %q", w.Code, w.Header().Get("Location"))
	}
}
func TestSetupRequiresTokenAndCreatesHashedAdmin(t *testing.T) {
	server, store := testServer(t, "correct")
	form := validSetupForm()
	form.Set("setup_token", "wrong")
	w := submit(server.Handler(), form)
	if w.Code != 422 {
		t.Fatalf("wrong token response %d", w.Code)
	}
	form.Set("setup_token", "correct")
	w = submit(server.Handler(), form)
	if w.Code != 303 || w.Header().Get("Location") != "/login?setup=complete" {
		t.Fatalf("response %d %q", w.Code, w.Header().Get("Location"))
	}
	required, err := store.SetupRequired(t.Context())
	if err != nil || required {
		t.Fatalf("setup required (%v,%v)", required, err)
	}
}
func TestSetupRejectsCrossOrigin(t *testing.T) {
	server, _ := testServer(t, "")
	form := validSetupForm()
	r := httptest.NewRequest("POST", "https://herring.example/setup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("response %d", w.Code)
	}
}

func TestSetupAcceptsConfiguredPublicOriginBehindProxy(t *testing.T) {
	server, _ := testServer(t, "")
	server.config.PublicOrigin = "https://xn--led-bza2n.mleczki.pl"
	form := validSetupForm()
	r := httptest.NewRequest("POST", "http://herring:8080/setup", strings.NewReader(form.Encode()))
	r.Host = "herring:8080"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://xn--led-bza2n.mleczki.pl")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
}
func validSetupForm() url.Values {
	return url.Values{"display_name": {"Michał"}, "email": {"admin@example.com"}, "password": {"correct horse battery staple"}, "password_confirmation": {"correct horse battery staple"}}
}
func submit(h http.Handler, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/setup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
