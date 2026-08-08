package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type setupStoreStub struct {
	required     bool
	createCalls  int
	email        string
	displayName  string
	passwordHash string
}

func (s *setupStoreStub) SetupRequired(context.Context) (bool, error) {
	return s.required, nil
}

func (s *setupStoreStub) CreateInitialAdmin(_ context.Context, email, displayName, passwordHash string) error {
	s.createCalls++
	s.email = email
	s.displayName = displayName
	s.passwordHash = passwordHash
	s.required = false
	return nil
}

func TestHomeRedirectsToFirstRunSetup(t *testing.T) {
	store := &setupStoreStub{required: true}
	response := httptest.NewRecorder()
	New(store, "", discardLogger()).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/setup" {
		t.Fatalf("response = %d location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestSetupRequiresConfiguredToken(t *testing.T) {
	store := &setupStoreStub{required: true}
	form := validSetupForm()
	form.Set("setup_token", "wrong")
	response := submitSetup(New(store, "correct-token", discardLogger()).Handler(), form)

	if response.Code != http.StatusUnprocessableEntity || store.createCalls != 0 {
		t.Fatalf("response = %d, create calls = %d", response.Code, store.createCalls)
	}
	if !strings.Contains(response.Body.String(), "setup token is invalid") {
		t.Errorf("response does not contain token error: %s", response.Body.String())
	}
}

func TestSetupCreatesHashedInitialAdministrator(t *testing.T) {
	store := &setupStoreStub{required: true}
	form := validSetupForm()
	form.Set("setup_token", "correct-token")
	response := submitSetup(New(store, "correct-token", discardLogger()).Handler(), form)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/?setup=complete" {
		t.Fatalf("response = %d location %q: %s", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	if store.createCalls != 1 || store.email != "admin@example.com" || store.displayName != "Michał" {
		t.Fatalf("unexpected stored admin: %#v", store)
	}
	if store.passwordHash == "correct horse battery staple" {
		t.Fatal("password was stored as plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(store.passwordHash), []byte("correct horse battery staple")); err != nil {
		t.Fatalf("stored password hash does not match: %v", err)
	}
}

func TestSetupRejectsCrossOriginSubmission(t *testing.T) {
	store := &setupStoreStub{required: true}
	form := validSetupForm()
	request := httptest.NewRequest(http.MethodPost, "https://herring.example/setup", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	New(store, "", discardLogger()).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || store.createCalls != 0 {
		t.Fatalf("response = %d, create calls = %d", response.Code, store.createCalls)
	}
}

func TestSetupIsClosedAfterAdminExists(t *testing.T) {
	store := &setupStoreStub{required: false}
	response := httptest.NewRecorder()
	New(store, "", discardLogger()).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/setup", nil))

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/" {
		t.Fatalf("response = %d location %q", response.Code, response.Header().Get("Location"))
	}
}

func validSetupForm() url.Values {
	return url.Values{
		"display_name":          {"Michał"},
		"email":                 {"Admin@Example.com"},
		"password":              {"correct horse battery staple"},
		"password_confirmation": {"correct horse battery staple"},
	}
}

func submitSetup(handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
