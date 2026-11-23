package httpapi

import (
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

const formType = "application/x-www-form-urlencoded"

var crossOrigin = map[string]string{"Origin": "https://attacker.example"}

func TestCreateDeviceRegistersTrackerID(t *testing.T) {
	h, store, sender, token, _ := smsServer(t)
	form := url.Values{"model": {"st901-2g"}, "phone": {"+48 600 100 201"}, "name": {"Rower"}, "tracker_id": {"816 052 8337"}}
	w := serve(h, "POST", "/devices", token, formType, form.Encode(), nil)
	if w.Code != 303 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	device, err := store.ManagedDeviceByPhone(t.Context(), "48600100201")
	if err != nil {
		t.Fatal(err)
	}
	if device.TrackerID != "8160528337" || device.ConfigStatus != "awaiting_reply" {
		t.Fatalf("device = %+v", device)
	}
	if len(sender.sent) != 7 {
		t.Fatalf("sent %d configuration SMS, want 7", len(sender.sent))
	}
	// The ID is registered, so the tracker's positions are accepted.
	if err := store.SaveLocation(t.Context(), time.Now(), testLocation(t, "8160528337")); err != nil {
		t.Fatalf("position from the registered ID: %v", err)
	}
}

func TestCreateDeviceRejectsBadOrTakenInput(t *testing.T) {
	h, store, sender, token, deviceID := smsServer(t)
	if err := store.UpdateManagedDevice(t.Context(), deviceID, "Auto", "8160528336"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		form url.Values
		code int
	}{
		{url.Values{"model": {"st901-2g"}, "phone": {"48600100201"}, "tracker_id": {"81605a"}}, 422},
		{url.Values{"model": {"st901-2g"}, "phone": {"48600100201"}, "tracker_id": {"8160528336"}}, 409},
		{url.Values{"model": {"st901-2g"}, "phone": {"+48600100200"}}, 409},
		{url.Values{"model": {"st901-2g"}, "phone": {"48600100201"}, "name": {strings.Repeat("x", 81)}}, 422},
	} {
		if w := serve(h, "POST", "/devices", token, formType, c.form.Encode(), nil); w.Code != c.code {
			t.Errorf("create %v: %d, want %d (%s)", c.form, w.Code, c.code, w.Body.String())
		}
	}
	if len(sender.sent) != 0 {
		t.Fatalf("rejected forms sent SMS: %v", sender.sent)
	}
	if devices, _ := store.ManagedDevices(t.Context()); len(devices) != 1 {
		t.Fatalf("rejected forms created devices: %+v", devices)
	}
}

func TestDeviceEndpointsRequireSessionAndSameOrigin(t *testing.T) {
	h, _, sender, token, deviceID := smsServer(t)
	base := "/devices/" + strconv.FormatInt(deviceID, 10)
	form := url.Values{"name": {"x"}, "confirm": {"1"}}.Encode()
	for _, path := range []string{"/devices", base, base + "/configure", base + "/delete"} {
		if w := serve(h, "POST", path, "", formType, form, nil); w.Code != 401 {
			t.Errorf("anonymous POST %s: %d", path, w.Code)
		}
		if w := serve(h, "POST", path, token, formType, form, crossOrigin); w.Code != 403 {
			t.Errorf("cross-origin POST %s: %d", path, w.Code)
		}
	}
	if w := serve(h, "GET", "/urzadzenia", "", "", "", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Errorf("anonymous devices page: %d %q", w.Code, w.Header().Get("Location"))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("rejected requests sent SMS: %v", sender.sent)
	}
}

func TestUpdateDeviceRenamesAndAssignsTracker(t *testing.T) {
	h, store, _, token, deviceID := smsServer(t)
	path := "/devices/" + strconv.FormatInt(deviceID, 10)
	w := serve(h, "POST", path, token, formType, url.Values{"name": {"Samochód"}, "tracker_id": {"8160528336"}}.Encode(), nil)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/urzadzenia?stan=zapisano") {
		t.Fatalf("update: %d %q %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	device, _ := store.ManagedDevice(t.Context(), deviceID)
	if device.Name != "Samochód" || device.TrackerID != "8160528336" {
		t.Fatalf("device = %+v", device)
	}
	if w := serve(h, "POST", path, token, formType, url.Values{"tracker_id": {"abc"}}.Encode(), nil); w.Code != 422 || !strings.Contains(w.Body.String(), "od 5 do 20 cyfr") {
		t.Fatalf("invalid tracker ID: %d", w.Code)
	}
	other, _ := store.CreateManagedDevice(t.Context(), "Rower", "st901-2g", "48600100201")
	if w := serve(h, "POST", "/devices/"+strconv.FormatInt(other.ID, 10), token, formType, url.Values{"tracker_id": {"8160528336"}}.Encode(), nil); w.Code != 409 {
		t.Fatalf("taken tracker ID: %d", w.Code)
	}
	if w := serve(h, "POST", "/devices/999", token, formType, "name=x", nil); w.Code != 404 {
		t.Fatalf("unknown device: %d", w.Code)
	}
}

func TestReconfigureDeviceResendsAndIsRateLimited(t *testing.T) {
	h, store, sender, token, deviceID := smsServer(t)
	if err := store.SetConfigurationStatus(t.Context(), deviceID, "failed", "Nie udało się."); err != nil {
		t.Fatal(err)
	}
	path := "/devices/" + strconv.FormatInt(deviceID, 10) + "/configure"
	w := serve(h, "POST", path, token, formType, "", nil)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/urzadzenia?stan=wyslano") {
		t.Fatalf("reconfigure: %d %s", w.Code, w.Body.String())
	}
	if len(sender.sent) != 7 || !strings.HasPrefix(sender.sent[0], "48600100200 ") {
		t.Fatalf("sent = %v", sender.sent)
	}
	if device, _ := store.ManagedDevice(t.Context(), deviceID); device.ConfigStatus != "awaiting_reply" {
		t.Fatalf("status = %q", device.ConfigStatus)
	}
	if w := serve(h, "POST", path, token, formType, "", nil); w.Code != 429 || len(sender.sent) != 7 {
		t.Fatalf("second reconfigure: %d, sent %d", w.Code, len(sender.sent))
	}
	if w := serve(h, "POST", "/devices/999/configure", token, formType, "", nil); w.Code != 404 {
		t.Fatalf("unknown device: %d", w.Code)
	}
}

func TestReconfigureWithoutSMSGateway(t *testing.T) {
	server, store := testServer(t, "")
	token := loginSession(t, store)
	device, _ := store.CreateManagedDevice(t.Context(), "Auto", "st901-4g", "48600100200")
	if w := serve(server.Handler(), "POST", "/devices/"+strconv.FormatInt(device.ID, 10)+"/configure", token, formType, "", nil); w.Code != 503 {
		t.Fatalf("reconfigure without gateway: %d", w.Code)
	}
}

func TestDeleteDeviceNeedsConfirmation(t *testing.T) {
	h, store, _, token, deviceID := smsServer(t)
	if err := store.UpdateManagedDevice(t.Context(), deviceID, "Auto", "8160528336"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(t.Context(), time.Now(), testLocation(t, "8160528336")); err != nil {
		t.Fatal(err)
	}
	path := "/devices/" + strconv.FormatInt(deviceID, 10) + "/delete"
	if w := serve(h, "POST", path, token, formType, "", nil); w.Code != 422 {
		t.Fatalf("delete without confirmation: %d", w.Code)
	}
	if _, err := store.ManagedDevice(t.Context(), deviceID); err != nil {
		t.Fatalf("device deleted without confirmation: %v", err)
	}
	w := serve(h, "POST", path, token, formType, "confirm=1", nil)
	if w.Code != 303 || w.Header().Get("Location") != "/urzadzenia?stan=usunieto" {
		t.Fatalf("delete: %d %q", w.Code, w.Header().Get("Location"))
	}
	if _, err := store.ManagedDevice(t.Context(), deviceID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("device still exists: %v", err)
	}
	if count, _ := store.CountPositions(t.Context()); count != 0 {
		t.Fatalf("positions kept: %d", count)
	}
	if w := serve(h, "POST", path, token, formType, "confirm=1", nil); w.Code != 404 {
		t.Fatalf("second delete: %d", w.Code)
	}
}

func TestPagesOfferRecentUnknownTrackers(t *testing.T) {
	h, store, _, token, _ := smsServer(t)
	if err := store.RecordUnknownTracker(t.Context(), "8160528399", time.Now().Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/urzadzenia"} {
		w := serve(h, "GET", path, token, "", "", nil)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `<option value="8160528399">`) || !strings.Contains(body, "3 min temu") {
			t.Errorf("GET %s: %d, unknown tracker not offered", path, w.Code)
		}
	}
	// Assigning it removes it from the list.
	device, _ := store.CreateManagedDevice(t.Context(), "Rower", "st901-2g", "48600100201")
	if err := store.UpdateManagedDevice(t.Context(), device.ID, "Rower", "8160528399"); err != nil {
		t.Fatal(err)
	}
	if w := serve(h, "GET", "/urzadzenia", token, "", "", nil); strings.Contains(w.Body.String(), `<option value="8160528399">`) {
		t.Fatal("assigned tracker still offered")
	}
}

func TestTimeAgoAndAttempts(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{10 * time.Second: "przed chwilą", 5 * time.Minute: "5 min temu", 3 * time.Hour: "3 godz. temu", 30 * time.Hour: "wczoraj", 72 * time.Hour: "3 dni temu"} {
		if got := timeAgo(now, now.Add(-d)); got != want {
			t.Errorf("timeAgo(%v) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int64]string{1: "1 próba", 3: "3 próby", 5: "5 prób", 12: "12 prób", 22: "22 próby"} {
		if got := attemptsLabel(n); got != want {
			t.Errorf("attemptsLabel(%d) = %q, want %q", n, got, want)
		}
	}
}

func testLocation(t *testing.T, trackerID string) sinotrack.Location {
	t.Helper()
	location, err := sinotrack.ParseLocation("*HQ," + trackerID + ",V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	if err != nil {
		t.Fatal(err)
	}
	return location
}
