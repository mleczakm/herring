package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mleczakm/herring/internal/storage/sqlite"
	"github.com/mleczakm/herring/internal/tracker/st901"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeSender) Send(_ context.Context, to, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, to+" "+body)
	return "msg-" + strconv.Itoa(len(f.sent)), nil
}

func smsServer(t *testing.T) (http.Handler, *sqlite.Store, *fakeSender, string, int64) {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	sender := &fakeSender{}
	server := New(store, sender, Config{WebhookSecret: "hook", Tracker: st901.Profile{ControlNumber: "48500100200", APN: "internet", ServerHost: "65.108.44.244", ServerPort: 20115, MovingInterval: 20, StoppedInterval: 300}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	token := loginSession(t, store)
	device, err := store.CreateManagedDevice(t.Context(), "Auto", "st901-4g", "48600100200")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(t.Context(), "8160528336"); err != nil {
		t.Fatal(err)
	}
	return server.Handler(), store, sender, token, device.ID
}

func TestLocateSendsCommandAndIsRateLimited(t *testing.T) {
	h, _, sender, token, deviceID := smsServer(t)
	path := "/api/devices/" + strconv.FormatInt(deviceID, 10) + "/locate"
	if w := serve(h, "POST", path, "", "application/json", "{}", nil); w.Code != 401 {
		t.Fatalf("anonymous locate: %d", w.Code)
	}
	if w := serve(h, "POST", path, token, "application/x-www-form-urlencoded", "", nil); w.Code != 415 {
		t.Fatalf("form-encoded locate: %d", w.Code)
	}
	if w := serve(h, "POST", path, token, "application/json", "{}", nil); w.Code != 202 {
		t.Fatalf("locate: %d %s", w.Code, w.Body.String())
	}
	if len(sender.sent) != 1 || sender.sent[0] != "48600100200 6690000" {
		t.Fatalf("sent = %v", sender.sent)
	}
	w := serve(h, "POST", path, token, "application/json", "{}", nil)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || len(sender.sent) != 1 {
		t.Fatalf("second locate: %d, sent %v", w.Code, sender.sent)
	}
	if w := serve(h, "POST", "/api/devices/999/locate", token, "application/json", "{}", nil); w.Code != 404 {
		t.Fatalf("unknown device: %d", w.Code)
	}
}

func TestLocateWithoutSMSGateway(t *testing.T) {
	server, store := testServer(t, "")
	token := loginSession(t, store)
	device, _ := store.CreateManagedDevice(t.Context(), "Auto", "st901-4g", "48600100200")
	w := serve(server.Handler(), "POST", "/api/devices/"+strconv.FormatInt(device.ID, 10)+"/locate", token, "application/json", "{}", nil)
	if w.Code != 503 {
		t.Fatalf("locate without gateway: %d", w.Code)
	}
}

func TestWebhookStoresSMSPositionOnce(t *testing.T) {
	h, store, _, token, _ := smsServer(t)
	reply := `{"type":"MESSAGE","from":"+48600100200","to":"48500100200","body":"http://maps.google.com/maps?q=+52.21872,+21.00946 Date: 2026-11-22 Time:12:00 ID: 8160528336 Fix:A Speed:12KM/H Bat:4"}`
	for i := 0; i < 2; i++ {
		// The gateway may deliver the same message more than once.
		if w := serve(h, "POST", "/webhooks/sendly/hook", "", "application/json", reply, nil); w.Code != 200 {
			t.Fatalf("webhook delivery %d: %d", i, w.Code)
		}
	}
	if count, _ := store.CountPositions(t.Context()); count != 1 {
		t.Fatalf("stored %d positions, want 1", count)
	}
	w := serve(h, "GET", "/api/positions", token, "", "", nil)
	body := w.Body.String()
	if !strings.Contains(body, `"source":"sms"`) || !strings.Contains(body, `"has_position":true`) || !strings.Contains(body, `"speed_kph":12`) {
		t.Fatalf("positions = %s", body)
	}
}

func TestWebhookStillHandlesConfigurationReplies(t *testing.T) {
	h, store, _, _, deviceID := smsServer(t)
	rconf := `{"type":"MESSAGE","from":"48600100200","body":"ID:8160528336; MODE:GPRS; APN:internet; IP:65.108.44.244:20115"}`
	if w := serve(h, "POST", "/webhooks/sendly/hook", "", "application/json", rconf, nil); w.Code != 200 {
		t.Fatalf("webhook: %d", w.Code)
	}
	device, _ := store.ManagedDevice(t.Context(), deviceID)
	if device.ConfigStatus != "configured" {
		t.Fatalf("config status = %q", device.ConfigStatus)
	}
	if count, _ := store.CountPositions(t.Context()); count != 0 {
		t.Fatalf("configuration reply stored as a position")
	}
}
