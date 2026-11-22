package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestInitContactMigratesExistingTables(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")
	// Tables as they existed before offline alerts and SMS positions.
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE positions (id INTEGER PRIMARY KEY, device_id TEXT NOT NULL, tracker_time TEXT NOT NULL, received_at TEXT NOT NULL, gps_valid INTEGER NOT NULL, latitude REAL NOT NULL, longitude REAL NOT NULL, speed_kph REAL NOT NULL, heading REAL NOT NULL, status TEXT NOT NULL, mcc INTEGER NOT NULL, mnc INTEGER NOT NULL, raw_frame TEXT NOT NULL, UNIQUE (device_id, tracker_time, raw_frame)) STRICT`,
		`INSERT INTO positions VALUES (1,'1234567890','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1,52,21,0,0,'',260,1,'old')`,
		`CREATE TABLE alert_settings (user_id INTEGER NOT NULL, device_id INTEGER NOT NULL, entry INTEGER NOT NULL, exit INTEGER NOT NULL, speed_kph REAL NOT NULL, humor INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(user_id,device_id)) STRICT`,
	} {
		if _, err := old.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	_ = old.Close()

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var source string
	if err := store.database.QueryRow(`SELECT source FROM positions WHERE id = 1`).Scan(&source); err != nil || source != "gprs" {
		t.Fatalf("existing position source = %q, %v", source, err)
	}
	if _, err := store.database.Exec(`SELECT offline_minutes, sms_fallback FROM alert_settings`); err != nil {
		t.Fatalf("alert_settings not migrated: %v", err)
	}
	// Reopening must not try to add the columns again.
	_ = store.Close()
	if store, err = Open(ctx, path); err != nil {
		t.Fatalf("reopen after migration: %v", err)
	}
}

func offlineTestStore(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.CreateInitialAdmin(ctx, "admin@example.com", "Admin", "hash"); err != nil {
		t.Fatal(err)
	}
	user, _ := store.UserByEmail(ctx, "admin@example.com")
	device, err := store.CreateManagedDevice(ctx, "Auto", "st901-4g", "48600100200")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkTrackerIfUnambiguous(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	return store, user.ID, device.ID
}

func TestOfflineAlertFiresOncePerOutageAndRecovers(t *testing.T) {
	ctx := context.Background()
	store, userID, deviceID := offlineTestStore(t)
	if err := store.SaveDeviceAlerts(ctx, userID, deviceID, AlertSettings{OfflineMinutes: 30, SMSFallback: true}); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if events, err := store.TouchTracker(ctx, "1234567890", t0); err != nil || len(events) != 0 {
		t.Fatalf("first contact = %v, %v", events, err)
	}
	if events, _ := store.OfflineEvents(ctx, t0.Add(20*time.Minute)); len(events) != 0 {
		t.Fatalf("offline before threshold: %+v", events)
	}
	events, err := store.OfflineEvents(ctx, t0.Add(31*time.Minute))
	if err != nil || len(events) != 1 {
		t.Fatalf("offline events = %+v, %v", events, err)
	}
	e := events[0]
	if e.Kind != "offline" || e.DeviceID != deviceID || e.DeviceName != "Auto" || !e.SMSFallback || e.OfflineFor != 31*time.Minute || e.RevealsLocation() {
		t.Fatalf("offline event = %+v", e)
	}
	if events, _ := store.OfflineEvents(ctx, t0.Add(2*time.Hour)); len(events) != 0 {
		t.Fatalf("same outage reported twice: %+v", events)
	}
	events, err = store.TouchTracker(ctx, "1234567890", t0.Add(3*time.Hour))
	if err != nil || len(events) != 1 || events[0].Kind != "online" {
		t.Fatalf("recovery events = %+v, %v", events, err)
	}
	if events, _ := store.TouchTracker(ctx, "1234567890", t0.Add(3*time.Hour+time.Minute)); len(events) != 0 {
		t.Fatalf("recovery repeated: %+v", events)
	}
	if last, ok, _ := store.LastContact(ctx, deviceID); !ok || !last.Equal(t0.Add(3*time.Hour+time.Minute)) {
		t.Fatalf("last contact = %v, %v", last, ok)
	}
}

func TestOfflineThresholdValidation(t *testing.T) {
	ctx := context.Background()
	store, userID, deviceID := offlineTestStore(t)
	for _, minutes := range []int{5, MaxOfflineMinutes + 1, -1} {
		if err := store.SaveDeviceAlerts(ctx, userID, deviceID, AlertSettings{OfflineMinutes: minutes}); err == nil {
			t.Errorf("accepted offline threshold %d", minutes)
		}
	}
	if err := store.SaveDeviceAlerts(ctx, userID, deviceID, AlertSettings{OfflineMinutes: 45, SMSFallback: true}); err != nil {
		t.Fatal(err)
	}
	settings, err := store.DeviceAlerts(ctx, userID, deviceID)
	if err != nil || settings.OfflineMinutes != 45 || !settings.SMSFallback {
		t.Fatalf("settings = %+v, %v", settings, err)
	}
}

func TestSaveSMSPositionLinksTrackerAndIgnoresRepeats(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	device, _ := store.CreateManagedDevice(ctx, "Auto", "st901-4g", "48600100200")
	other, _ := store.CreateManagedDevice(ctx, "Rower", "st901-2g", "48600100201")
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	received := time.Date(2026, 11, 22, 12, 0, 30, 0, time.UTC)
	p := SMSPosition{TrackerID: "1234567890", Latitude: 52.2, Longitude: 21.0, SpeedKPH: 12, GPSValid: true, TrackerTime: received.Truncate(time.Minute), Raw: "http://maps.google.com/maps?q=52.2,21.0"}

	if _, err := store.SaveSMSPosition(ctx, other.ID, SMSPosition{TrackerID: "9999999999", Latitude: 1, Longitude: 1, TrackerTime: received, Raw: "x"}, received); !errors.Is(err, ErrTrackerNotLinked) {
		t.Fatalf("unregistered tracker ID: %v", err)
	}
	inserted, err := store.SaveSMSPosition(ctx, device.ID, p, received)
	if err != nil || !inserted {
		t.Fatalf("first SMS position: %v, %v", inserted, err)
	}
	if inserted, err := store.SaveSMSPosition(ctx, device.ID, p, received.Add(time.Second)); err != nil || inserted {
		t.Fatalf("repeated webhook delivery stored again: %v, %v", inserted, err)
	}
	late := p
	late.TrackerTime = received.Add(10 * time.Minute)
	if inserted, err := store.SaveSMSPosition(ctx, device.ID, late, received.Add(10*time.Minute)); err != nil || inserted {
		t.Fatalf("late retry with a new fallback time stored again: %v, %v", inserted, err)
	}
	positions, _ := store.LatestPositions(ctx)
	for _, dp := range positions {
		if dp.Device.ID == device.ID && (!dp.HasPosition || dp.Source != "sms" || dp.Device.TrackerID != "1234567890") {
			t.Fatalf("latest position = %+v", dp)
		}
	}
	// The claimed tracker ID cannot be linked to a second device.
	if _, err := store.SaveSMSPosition(ctx, other.ID, p, received); !errors.Is(err, ErrTrackerNotLinked) {
		t.Fatalf("second device claimed a linked tracker: %v", err)
	}
	if count, _ := store.CountSMSCommandsSince(ctx, received.Add(-time.Hour)); count != 0 {
		t.Fatalf("SMS count = %d", count)
	}
}
