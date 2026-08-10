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
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
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
func TestSetupAcceptsNullOriginFromNoReferrerPolicy(t *testing.T) {
	server, _ := testServer(t, "")
	server.config.PublicOrigin = "https://xn--led-bza2n.mleczki.pl"
	form := validSetupForm()
	r := httptest.NewRequest("POST", "https://xn--led-bza2n.mleczki.pl/setup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "null")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
}
func loginSession(t *testing.T, store *sqlite.Store) string {
	t.Helper()
	if err := store.CreateInitialAdmin(t.Context(), "admin@example.com", "Admin", "hash"); err != nil {
		t.Fatal(err)
	}
	user, err := store.UserByEmail(t.Context(), "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token := randomToken()
	if err := store.CreateSession(t.Context(), tokenHash(token), user.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return token
}
func TestPositionsRequiresAuthentication(t *testing.T) {
	server, _ := testServer(t, "")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/positions", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("response %d", w.Code)
	}
}
func TestPositionsReturnsLinkedDevicePosition(t *testing.T) {
	server, store := testServer(t, "")
	token := loginSession(t, store)
	if _, err := store.CreateManagedDevice(t.Context(), "Rower", "st901-2g", "+48500600700"); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(t.Context(), "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkTrackerIfUnambiguous(t.Context(), "1234567890"); err != nil {
		t.Fatal(err)
	}
	location, err := sinotrack.ParseLocation("*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(t.Context(), time.Now(), location); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/api/positions", nil)
	r.AddCookie(&http.Cookie{Name: "herring_session", Value: token})
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("response %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"has_position":true`) {
		t.Fatalf("body = %s", w.Body.String())
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
