package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func saveTestPosition(t *testing.T, store *Store, receivedAt time.Time, frameTime string) {
	t.Helper()
	location, err := sinotrack.ParseLocation("*HQ,1234567890,V1," + frameTime + ",A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(context.Background(), receivedAt, location); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsDefaultsAndRetentionValidation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	settings, err := store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.PrivacyMode || settings.ServerRetentionDays != 0 || settings.BrowserRetentionDays != 0 {
		t.Fatalf("default settings = %+v", settings)
	}
	if err := store.SaveRetention(ctx, 30, 365); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][2]int{{-1, 0}, {0, MaxRetentionDays + 1}} {
		if err := store.SaveRetention(ctx, invalid[0], invalid[1]); err == nil {
			t.Errorf("SaveRetention(%d, %d) accepted invalid retention", invalid[0], invalid[1])
		}
	}
	settings, _ = store.Settings(ctx)
	if settings.ServerRetentionDays != 30 || settings.BrowserRetentionDays != 365 {
		t.Fatalf("retention = %d/%d, want 30/365", settings.ServerRetentionDays, settings.BrowserRetentionDays)
	}
}

func TestPrivacyBindingCanBeReplacedAndDisabled(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	if err := store.EnablePrivacy(ctx, "hash-a", "Chrome · Android", now); err != nil {
		t.Fatal(err)
	}
	if err := store.EnablePrivacy(ctx, "hash-b", "Safari · iOS", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	settings, _ := store.Settings(ctx)
	if !settings.PrivacyMode || settings.PrivacyClientHash != "hash-b" || settings.PrivacyClientLabel != "Safari · iOS" || !settings.PrivacyBoundAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("settings after rebinding = %+v", settings)
	}
	if err := store.DisablePrivacy(ctx); err != nil {
		t.Fatal(err)
	}
	settings, _ = store.Settings(ctx)
	if settings.PrivacyMode || settings.PrivacyClientHash != "" {
		t.Fatalf("settings after disabling = %+v", settings)
	}
}

func TestPurgePositionsReceivedBefore(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	saveTestPosition(t, store, now.Add(-40*24*time.Hour), "100000")
	saveTestPosition(t, store, now.Add(-time.Hour), "110000")
	deleted, err := store.PurgePositionsReceivedBefore(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	remaining, _ := store.OldestPositions(ctx, 10)
	if deleted != 1 || len(remaining) != 1 || remaining[0].TrackerTime.Hour() != 11 {
		t.Fatalf("deleted %d, remaining %+v", deleted, remaining)
	}
}

func TestDeletePositionsIgnoresReusedRowID(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	saveTestPosition(t, store, time.Now(), "100000")
	first, _ := store.OldestPositions(ctx, 10)
	key := PositionKey{ID: first[0].ID, DeviceID: first[0].DeviceID, TrackerTime: first[0].TrackerTime}
	if n, err := store.DeletePositions(ctx, []PositionKey{key}); err != nil || n != 1 {
		t.Fatalf("first delete = %d, %v", n, err)
	}
	// The table is empty, so SQLite hands the same rowid to the next position.
	saveTestPosition(t, store, time.Now(), "110000")
	second, _ := store.OldestPositions(ctx, 10)
	if len(second) != 1 || second[0].ID != key.ID {
		t.Fatalf("expected rowid reuse, got %+v", second)
	}
	// A repeated acknowledgement for the old position must not delete it.
	if n, err := store.DeletePositions(ctx, []PositionKey{key}); err != nil || n != 0 {
		t.Fatalf("stale delete = %d, %v", n, err)
	}
	if remaining, _ := store.OldestPositions(ctx, 10); len(remaining) != 1 {
		t.Fatalf("new position was deleted by a stale acknowledgement")
	}
}

func TestApplyServerRetentionHonoursUnlimited(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	saveTestPosition(t, store, now.AddDate(0, 0, -400), "100000")
	if n, err := store.ApplyServerRetention(ctx, now); err != nil || n != 0 {
		t.Fatalf("unlimited retention deleted %d, %v", n, err)
	}
	if err := store.SaveRetention(ctx, 365, 0); err != nil {
		t.Fatal(err)
	}
	if n, err := store.ApplyServerRetention(ctx, now); err != nil || n != 1 {
		t.Fatalf("365-day retention deleted %d, %v", n, err)
	}
	if count, _ := store.CountPositions(ctx); count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}
