package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func TestInitialAdminCanOnlyBeCreatedOnce(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	required, err := store.SetupRequired(ctx)
	if err != nil || !required {
		t.Fatalf("SetupRequired() = (%v, %v), want (true, nil)", required, err)
	}
	if err := store.CreateInitialAdmin(ctx, " Admin@Example.com ", " Michał ", "password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateInitialAdmin(ctx, "other@example.com", "Other", "other-hash"); !errors.Is(err, ErrSetupComplete) {
		t.Fatalf("second CreateInitialAdmin() error = %v, want %v", err, ErrSetupComplete)
	}
	required, err = store.SetupRequired(ctx)
	if err != nil || required {
		t.Fatalf("SetupRequired() = (%v, %v), want (false, nil)", required, err)
	}

	var email, displayName, role, passwordHash string
	if err := store.database.QueryRow(`
        SELECT email, display_name, role, password_hash FROM users`).Scan(
		&email, &displayName, &role, &passwordHash,
	); err != nil {
		t.Fatal(err)
	}
	if email != "admin@example.com" || displayName != "Michał" || role != "admin" || passwordHash != "password-hash" {
		t.Errorf("unexpected admin: email=%q name=%q role=%q hash=%q", email, displayName, role, passwordHash)
	}
}

func TestStorePersistsLocationAndIgnoresExactDuplicate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "herring.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	location, err := sinotrack.ParseLocation(
		"*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#",
	)
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Date(2026, 8, 8, 12, 0, 1, 0, time.UTC)
	if err := store.SaveLocation(ctx, receivedAt, location); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(ctx, receivedAt.Add(time.Second), location); err != nil {
		t.Fatal(err)
	}

	var count int
	var trackerTime, storedReceivedAt string
	err = store.database.QueryRowContext(ctx, `
        SELECT COUNT(*), tracker_time, received_at FROM positions WHERE device_id = ?`,
		location.DeviceID,
	).Scan(&count, &trackerTime, &storedReceivedAt)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("position count = %d, want 1", count)
	}
	if trackerTime != formatTime(location.TrackerTime) || storedReceivedAt != formatTime(receivedAt) {
		t.Errorf("stored times = (%q, %q)", trackerTime, storedReceivedAt)
	}
}

func TestStoreRejectsUnknownDevice(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	location, err := sinotrack.ParseLocation(
		"*HQ,unknown,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(ctx, time.Now(), location); err == nil {
		t.Fatal("SaveLocation() accepted an unknown device")
	}
}

func TestLinkTrackerIfUnambiguousLinksSoleAwaitingDevice(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	device, err := store.CreateManagedDevice(ctx, "Rower", "st901-2g", "+48500600700")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkTrackerIfUnambiguous(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	location, err := sinotrack.ParseLocation(
		"*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(ctx, time.Now(), location); err != nil {
		t.Fatal(err)
	}

	positions, err := store.LatestPositions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || positions[0].Device.ID != device.ID {
		t.Fatalf("positions = %+v", positions)
	}
	if !positions[0].HasPosition {
		t.Fatalf("expected linked device to have a position: %+v", positions[0])
	}
	if positions[0].Device.TrackerID != "1234567890" {
		t.Errorf("tracker_id = %q, want 1234567890", positions[0].Device.TrackerID)
	}
}

func TestLinkTrackerIfUnambiguousSkipsWhenMultipleAwaiting(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.CreateManagedDevice(ctx, "Rower", "st901-2g", "+48500600700"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedDevice(ctx, "Auto", "st901-4g", "+48500600701"); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkTrackerIfUnambiguous(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}

	positions, err := store.LatestPositions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range positions {
		if p.Device.TrackerID != "" {
			t.Errorf("device %d linked ambiguously: tracker_id=%q", p.Device.ID, p.Device.TrackerID)
		}
	}
}

func TestOpenEnablesRequiredPragmas(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for pragma, want := range map[string]string{
		"foreign_keys": "1",
		"journal_mode": "wal",
		"busy_timeout": "5000",
	} {
		var got string
		if err := store.database.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("read PRAGMA %s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %q, want %q", pragma, got, want)
		}
	}
}
