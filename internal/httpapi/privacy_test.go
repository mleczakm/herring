package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

const testClientKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func trackedServer(t *testing.T) (http.Handler, *sqlite.Store, string) {
	t.Helper()
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
	for _, frameTime := range []string{"120000", "120100"} {
		location, err := sinotrack.ParseLocation("*HQ,1234567890,V1," + frameTime + ",A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveLocation(t.Context(), time.Now(), location); err != nil {
			t.Fatal(err)
		}
	}
	return server.Handler(), store, token
}

func serve(h http.Handler, method, path, token, contentType, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "herring_session", Value: token})
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func enableTestPrivacy(t *testing.T, h http.Handler, token string) {
	t.Helper()
	w := serve(h, "POST", "/api/privacy/enable", token, "application/json", `{"client_key":"`+testClientKey+`","label":"Chrome · Android"}`, nil)
	if w.Code != 204 {
		t.Fatalf("enable response %d: %s", w.Code, w.Body.String())
	}
}

func TestPrivacyEnableRejectsFormsCrossOriginAndBadKeys(t *testing.T) {
	h, store, token := trackedServer(t)
	cases := []struct {
		name, contentType, body string
		headers                 map[string]string
		want                    int
	}{
		{"form post", "application/x-www-form-urlencoded", "client_key=" + testClientKey, nil, 415},
		{"cross origin", "application/json", `{"client_key":"` + testClientKey + `"}`, map[string]string{"Origin": "https://evil.example"}, 403},
		{"short key", "application/json", `{"client_key":"abc"}`, nil, 400},
	}
	for _, c := range cases {
		if w := serve(h, "POST", "/api/privacy/enable", token, c.contentType, c.body, c.headers); w.Code != c.want {
			t.Errorf("%s: response %d, want %d", c.name, w.Code, c.want)
		}
	}
	if w := serve(h, "POST", "/api/privacy/enable", "", "application/json", `{"client_key":"`+testClientKey+`"}`, nil); w.Code != 401 {
		t.Errorf("anonymous enable: response %d, want 401", w.Code)
	}
	if settings, _ := store.Settings(t.Context()); settings.PrivacyMode {
		t.Fatal("privacy mode enabled by a rejected request")
	}
}

func TestPrivacyModeTransfersPositionsOnlyToBoundBrowser(t *testing.T) {
	h, store, token := trackedServer(t)
	enableTestPrivacy(t, h, token)

	// The shared endpoint no longer exposes positions to any session.
	w := serve(h, "GET", "/api/positions", token, "", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"has_position":true`) || strings.Contains(w.Body.String(), "latitude") {
		t.Fatalf("positions in privacy mode: %d %s", w.Code, w.Body.String())
	}
	// Without the browser secret, or with another one, sync is refused.
	if w := serve(h, "GET", "/api/privacy/sync", token, "", "", nil); w.Code != 403 {
		t.Fatalf("sync without client key: %d", w.Code)
	}
	other := map[string]string{clientHeader: strings.Repeat("f", 64)}
	if w := serve(h, "GET", "/api/privacy/sync", token, "", "", other); w.Code != 403 {
		t.Fatalf("sync with another client key: %d", w.Code)
	}
	bound := map[string]string{clientHeader: testClientKey}
	if w := serve(h, "GET", "/api/privacy/sync", "", "", "", bound); w.Code != 401 {
		t.Fatalf("sync without session: %d", w.Code)
	}

	w = serve(h, "GET", "/api/privacy/sync", token, "", "", bound)
	if w.Code != 200 {
		t.Fatalf("sync response %d: %s", w.Code, w.Body.String())
	}
	var batch struct {
		Devices   []privacyDevice   `json:"devices"`
		Positions []privacyPosition `json:"positions"`
		More      bool              `json:"more"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Positions) != 2 || batch.More || len(batch.Devices) != 1 || batch.Devices[0].TrackerID != "1234567890" {
		t.Fatalf("batch = %+v", batch)
	}

	// Acknowledging the batch removes it from the server.
	ack, _ := json.Marshal(map[string]any{"positions": batch.Positions})
	w = serve(h, "POST", "/api/privacy/ack", token, "application/json", string(ack), bound)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"deleted":2`) {
		t.Fatalf("ack response %d: %s", w.Code, w.Body.String())
	}
	if count, _ := store.CountPositions(t.Context()); count != 0 {
		t.Fatalf("server still stores %d positions", count)
	}

	// A browser bound later replaces the first one.
	second := strings.Repeat("a", 64)
	if w := serve(h, "POST", "/api/privacy/enable", token, "application/json", `{"client_key":"`+second+`"}`, nil); w.Code != 204 {
		t.Fatalf("rebind response %d", w.Code)
	}
	if w := serve(h, "GET", "/api/privacy/sync", token, "", "", bound); w.Code != 403 {
		t.Fatalf("old browser still syncs after rebinding: %d", w.Code)
	}

	// Once disabled, sync reports that privacy mode is off.
	if w := serve(h, "POST", "/api/privacy/disable", token, "application/json", "{}", nil); w.Code != 204 {
		t.Fatalf("disable response %d", w.Code)
	}
	if w := serve(h, "GET", "/api/privacy/sync", token, "", "", map[string]string{clientHeader: second}); w.Code != 409 {
		t.Fatalf("sync after disable: %d", w.Code)
	}
}

func TestPrivacyAckRequiresBoundBrowser(t *testing.T) {
	h, store, token := trackedServer(t)
	enableTestPrivacy(t, h, token)
	positions, _ := store.OldestPositions(t.Context(), 10)
	body := `{"positions":[{"id":` + jsonNumber(positions[0].ID) + `,"device_id":"1234567890","tracker_time":"` + positions[0].TrackerTime.Format(time.RFC3339Nano) + `"}]}`
	if w := serve(h, "POST", "/api/privacy/ack", token, "application/json", body, map[string]string{clientHeader: strings.Repeat("f", 64)}); w.Code != 403 {
		t.Fatalf("ack from unbound browser: %d", w.Code)
	}
	if count, _ := store.CountPositions(t.Context()); count != 2 {
		t.Fatalf("unbound ack deleted positions, %d left", count)
	}
}

func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestSettingsSaveRetentionAppliesImmediately(t *testing.T) {
	h, store, token := trackedServer(t)
	if _, err := store.PurgePositionsReceivedBefore(t.Context(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	location, _ := sinotrack.ParseLocation("*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	if err := store.SaveLocation(t.Context(), time.Now().AddDate(0, 0, -10), location); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"server_retention_days": {"abc"}, "browser_retention_days": {"30"}}
	if w := serve(h, "POST", "/ustawienia", token, "application/x-www-form-urlencoded", form.Encode(), nil); w.Code != 422 {
		t.Fatalf("invalid retention response %d", w.Code)
	}
	form.Set("server_retention_days", "7")
	w := serve(h, "POST", "/ustawienia", token, "application/x-www-form-urlencoded", form.Encode(), nil)
	if w.Code != 303 || w.Header().Get("Location") != "/ustawienia?saved=1" {
		t.Fatalf("save response %d %s", w.Code, w.Header().Get("Location"))
	}
	settings, _ := store.Settings(t.Context())
	if settings.ServerRetentionDays != 7 || settings.BrowserRetentionDays != 30 {
		t.Fatalf("settings = %+v", settings)
	}
	if count, _ := store.CountPositions(t.Context()); count != 0 {
		t.Fatalf("10-day-old position survived a 7-day retention: %d left", count)
	}
	if w := serve(h, "POST", "/ustawienia", "", "application/x-www-form-urlencoded", form.Encode(), nil); w.Code != 401 {
		t.Fatalf("anonymous save response %d", w.Code)
	}

	page := serve(h, "GET", "/ustawienia?saved=1", token, "", "", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Zapisano ustawienia") || !strings.Contains(page.Body.String(), `value="7"`) {
		t.Fatalf("settings page %d", page.Code)
	}
}

func TestServiceWorkerAndManifestAreRevalidated(t *testing.T) {
	server, _ := testServer(t, "")
	h := server.Handler()
	for path, want := range map[string]string{"/sw.js": "notificationclick", "/manifest.webmanifest": `"display": "standalone"`} {
		w := serve(h, "GET", path, "", "", "", nil)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s: %d, Cache-Control %q", path, w.Code, w.Header().Get("Cache-Control"))
		}
	}
}
