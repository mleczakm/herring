package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlacesAPIAuthenticationAndOrigin(t *testing.T) {
	server, store := testServer(t, "")
	token := loginSession(t, store)
	mux := server.Handler()
	call := func(method, path, body, session, origin string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://herring.example"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "herring_session", Value: session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/places", "", "", ""); w.Code != 401 {
		t.Fatalf("unauthenticated %d", w.Code)
	}
	body := `{"name":"Home","latitude":52,"longitude":21,"radius_m":100}`
	if w := call("POST", "/api/places", body, token, "https://attacker.example"); w.Code != 403 {
		t.Fatalf("cross-origin %d", w.Code)
	}
	if w := call("POST", "/api/places", body, token, "https://herring.example"); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	if w := call("GET", "/api/places", "", token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Home") {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	if w := call("DELETE", "/api/places/1", "{}", token, "https://herring.example"); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body.String())
	}
}
