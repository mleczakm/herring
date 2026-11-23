package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

const historyDay = "/api/history?device=1&from=2026-08-08T00:00:00Z&to=2026-08-09T00:00:00Z"

type historyResponse struct {
	Positions []struct {
		TrackerTime string  `json:"tracker_time"`
		SpeedKPH    float64 `json:"speed_kph"`
		Source      string  `json:"source"`
	} `json:"positions"`
	More          bool   `json:"more"`
	Oldest        string `json:"oldest"`
	RetentionDays int    `json:"retention_days"`
}

func TestHistoryReturnsValidFixesInRange(t *testing.T) {
	h, store, token := trackedServer(t)
	// A fix without GPS repeats a stale position and must not be drawn.
	invalid, err := sinotrack.ParseLocation("*HQ,1234567890,V1,120030,V,5213.1234,N,02100.5678,E,000.00,0,080826,FFFFFFFF,260,06#")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(t.Context(), time.Now(), invalid); err != nil {
		t.Fatal(err)
	}
	w := serve(h, "GET", historyDay, token, "", "", nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response %d %q: %s", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
	}
	var body historyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Positions) != 2 || body.Positions[0].TrackerTime != "2026-08-08T12:00:00Z" || body.Positions[1].TrackerTime != "2026-08-08T12:01:00Z" {
		t.Fatalf("positions = %+v", body.Positions)
	}
	if body.Positions[0].Source != "gprs" || body.More || body.Oldest != "2026-08-08T12:00:00Z" {
		t.Fatalf("body = %+v", body)
	}

	w = serve(h, "GET", "/api/history?device=1&from=2026-08-09T00:00:00Z&to=2026-08-10T00:00:00Z", token, "", "", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Positions) != 0 || body.Oldest == "" {
		t.Fatalf("empty day = %d %s", w.Code, w.Body.String())
	}
}

func TestHistoryValidatesRequest(t *testing.T) {
	h, _, token := trackedServer(t)
	if w := serve(h, "GET", historyDay, "", "", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous response %d", w.Code)
	}
	cases := map[string]int{
		"/api/history?device=x&from=2026-08-08T00:00:00Z&to=2026-08-09T00:00:00Z":  400,
		"/api/history?device=1&from=2026-08-09T00:00:00Z&to=2026-08-08T00:00:00Z":  400,
		"/api/history?device=1&from=2026-06-01T00:00:00Z&to=2026-08-09T00:00:00Z":  400,
		"/api/history?device=1&from=yesterday&to=2026-08-09T00:00:00Z":             400,
		historyDay + "&cursor=nonsense":                                            400,
		"/api/history?device=99&from=2026-08-08T00:00:00Z&to=2026-08-09T00:00:00Z": 404,
		"/api/history?device=1&from=2026-07-31T22:00:00Z&to=2026-09-01T22:00:00Z":  200,
		historyDay + "&cursor=" + "2026-08-08T12:00:00Z,1":                         200,
	}
	for path, want := range cases {
		if w := serve(h, "GET", path, token, "", "", nil); w.Code != want {
			t.Errorf("GET %s = %d, want %d: %s", path, w.Code, want, w.Body.String())
		}
	}
	w := serve(h, "GET", historyDay+"&cursor=2026-08-08T12:00:00Z,1", token, "", "", nil)
	var body historyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Positions) != 1 || body.Positions[0].TrackerTime != "2026-08-08T12:01:00Z" {
		t.Fatalf("after cursor = %s", w.Body.String())
	}
}

func TestHistoryInPrivacyModeIsOnlyInTheBrowser(t *testing.T) {
	h, _, token := trackedServer(t)
	enableTestPrivacy(t, h, token)
	w := serve(h, "GET", historyDay, token, "", "", nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "privacy_mode") || strings.Contains(w.Body.String(), "latitude") {
		t.Fatalf("privacy response %d: %s", w.Code, w.Body.String())
	}
	page := serve(h, "GET", "/historia", token, "", "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-privacy="local"`) {
		t.Fatalf("history page %d", page.Code)
	}
}

func TestHistoryPage(t *testing.T) {
	h, _, token := trackedServer(t)
	if w := serve(h, "GET", "/historia", "", "", "", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous page %d %q", w.Code, w.Header().Get("Location"))
	}
	w := serve(h, "GET", "/historia", token, "", "", nil)
	body := w.Body.String()
	for _, want := range []string{`id="history-form"`, `<option value="1" data-tracked>Rower</option>`, `class="nav-link active" href="/historia"`, `/assets/history.js?v=`} {
		if !strings.Contains(body, want) {
			t.Errorf("history page lacks %q", want)
		}
	}
	if js := serve(h, "GET", "/assets/history.js", "", "", "", nil); js.Code != 200 || !strings.Contains(js.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("history.js %d %q", js.Code, js.Header().Get("Content-Type"))
	}
	// The service worker precaches exactly the versions the pages reference.
	sw := serve(h, "GET", "/sw.js", "", "", "", nil).Body.String()
	for _, asset := range []string{"/assets/history.js?v=", "/assets/privacy.js?v=", "/assets/herring.css?v="} {
		start := strings.Index(body, asset)
		if start < 0 {
			t.Fatalf("page lacks %s", asset)
		}
		url := body[start : start+strings.IndexByte(body[start:], '"')]
		if !strings.Contains(sw, `"`+url+`"`) {
			t.Errorf("service worker does not precache %s", url)
		}
	}
}
