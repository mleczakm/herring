package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func testFrame(t *testing.T, trackerID string) sinotrack.Location {
	t.Helper()
	location, err := sinotrack.ParseLocation("*HQ," + trackerID + ",V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestNormalizeTrackerID(t *testing.T) {
	for input, want := range map[string]string{"8160528336": "8160528336", " 816 052 8336 ": "8160528336"} {
		if got, err := NormalizeTrackerID(input); err != nil || got != want {
			t.Errorf("NormalizeTrackerID(%q) = (%q, %v), want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "1234", "81605283a6", "816-052-8336", "123456789012345678901"} {
		if _, err := NormalizeTrackerID(input); !errors.Is(err, ErrInvalidTrackerID) {
			t.Errorf("NormalizeTrackerID(%q) error = %v, want ErrInvalidTrackerID", input, err)
		}
	}
}

func TestSaveLocationReportsUnknownTracker(t *testing.T) {
	store := openTestStore(t)
	if err := store.SaveLocation(t.Context(), time.Now(), testFrame(t, "8160528336")); !errors.Is(err, ErrUnknownTracker) {
		t.Fatalf("SaveLocation() error = %v, want ErrUnknownTracker", err)
	}
}

func TestUnknownTrackersAreRecordedBoundedAndForgotten(t *testing.T) {
	ctx := t.Context()
	store := openTestStore(t)
	now := time.Now()
	if err := store.RegisterDevice(ctx, "1111111111"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"8160528336", "8160528336", "1111111111", "not-a-number", "12"} {
		if err := store.RecordUnknownTracker(ctx, id, now); err != nil {
			t.Fatal(err)
		}
	}
	trackers, err := store.UnknownTrackers(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(trackers) != 1 || trackers[0].ID != "8160528336" || trackers[0].Attempts != 2 {
		t.Fatalf("UnknownTrackers() = %+v, want only 8160528336 with 2 attempts", trackers)
	}

	// Old entries drop out of the list, and a flood keeps only the newest.
	if trackers, _ := store.UnknownTrackers(ctx, now.Add(UnknownTrackerWindow+time.Minute)); len(trackers) != 0 {
		t.Fatalf("week-old trackers still listed: %+v", trackers)
	}
	for i := range 30 {
		if err := store.RecordUnknownTracker(ctx, fmt.Sprintf("90000000%02d", i), now.Add(time.Duration(i+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var stored int
	if err := store.database.QueryRow(`SELECT COUNT(*) FROM unknown_trackers`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != maxUnknownTrackers {
		t.Fatalf("stored %d unknown trackers, want %d", stored, maxUnknownTrackers)
	}
	trackers, _ = store.UnknownTrackers(ctx, now)
	if trackers[0].ID != "9000000029" {
		t.Fatalf("newest unknown tracker = %s", trackers[0].ID)
	}

	// Recording an unknown tracker never registers it.
	if err := store.SaveLocation(ctx, now, testFrame(t, "9000000029")); !errors.Is(err, ErrUnknownTracker) {
		t.Fatalf("SaveLocation() after recording = %v, want ErrUnknownTracker", err)
	}
}

func TestCreateManagedDeviceWithTrackerRegistersAndLinks(t *testing.T) {
	ctx := t.Context()
	store := openTestStore(t)
	if err := store.RecordUnknownTracker(ctx, "8160528336", time.Now()); err != nil {
		t.Fatal(err)
	}
	device, err := store.CreateManagedDeviceWithTracker(ctx, " Auto ", "st901-4g", "48600100200", "8160528336")
	if err != nil {
		t.Fatal(err)
	}
	if device.TrackerID != "8160528336" || device.Name != "Auto" {
		t.Fatalf("device = %+v", device)
	}
	if err := store.SaveLocation(ctx, time.Now(), testFrame(t, "8160528336")); err != nil {
		t.Fatalf("SaveLocation() for an assigned ID: %v", err)
	}
	if trackers, _ := store.UnknownTrackers(ctx, time.Now()); len(trackers) != 0 {
		t.Fatalf("assigned tracker still listed as unknown: %+v", trackers)
	}

	if _, err := store.CreateManagedDeviceWithTracker(ctx, "", "st901-4g", "48600100200", ""); !errors.Is(err, ErrPhoneInUse) {
		t.Fatalf("duplicate phone error = %v, want ErrPhoneInUse", err)
	}
	if _, err := store.CreateManagedDeviceWithTracker(ctx, "", "st901-4g", "48600100201", "8160528336"); !errors.Is(err, ErrTrackerInUse) {
		t.Fatalf("duplicate tracker error = %v, want ErrTrackerInUse", err)
	}
	if _, err := store.CreateManagedDeviceWithTracker(ctx, "", "st901-4g", "48600100202", "12ab"); !errors.Is(err, ErrInvalidTrackerID) {
		t.Fatalf("invalid tracker error = %v, want ErrInvalidTrackerID", err)
	}
	devices, _ := store.ManagedDevices(ctx)
	if len(devices) != 1 {
		t.Fatalf("rejected devices were created: %+v", devices)
	}
}

func TestAssignedTrackerDoesNotLinkToAnotherPendingDevice(t *testing.T) {
	ctx := t.Context()
	store := openTestStore(t)
	if _, err := store.CreateManagedDeviceWithTracker(ctx, "Auto", "st901-4g", "48600100200", "8160528336"); err != nil {
		t.Fatal(err)
	}
	pending, err := store.CreateManagedDevice(ctx, "Rower", "st901-2g", "48600100201")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"8160528336", "9999999999"} {
		if err := store.LinkTrackerIfUnambiguous(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := store.ManagedDevice(ctx, pending.ID)
	if got.TrackerID != "" {
		t.Fatalf("pending device linked to %q", got.TrackerID)
	}
}

func TestUpdateManagedDeviceRenamesAndChangesTracker(t *testing.T) {
	ctx := t.Context()
	store := openTestStore(t)
	device, err := store.CreateManagedDevice(ctx, "Auto", "st901-4g", "48600100200")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateManagedDevice(ctx, device.ID, "Samochód", "8160528336"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(ctx, time.Now(), testFrame(t, "8160528336")); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ManagedDevice(ctx, device.ID)
	if got.Name != "Samochód" || got.TrackerID != "8160528336" {
		t.Fatalf("device = %+v", got)
	}
	// An empty ID keeps the current one.
	if err := store.UpdateManagedDevice(ctx, device.ID, "Auto", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ManagedDevice(ctx, device.ID); got.TrackerID != "8160528336" || got.Name != "Auto" {
		t.Fatalf("device after rename = %+v", got)
	}
	// A new ID unregisters the old one together with its positions.
	if err := store.UpdateManagedDevice(ctx, device.ID, "Auto", "8160528337"); err != nil {
		t.Fatal(err)
	}
	if count, _ := store.CountPositions(ctx); count != 0 {
		t.Fatalf("positions of the old ID kept: %d", count)
	}
	if err := store.SaveLocation(ctx, time.Now(), testFrame(t, "8160528336")); !errors.Is(err, ErrUnknownTracker) {
		t.Fatalf("old ID still accepted: %v", err)
	}

	other, _ := store.CreateManagedDevice(ctx, "Rower", "st901-2g", "48600100201")
	if err := store.UpdateManagedDevice(ctx, other.ID, "Rower", "8160528337"); !errors.Is(err, ErrTrackerInUse) {
		t.Fatalf("taken ID error = %v, want ErrTrackerInUse", err)
	}
	if err := store.UpdateManagedDevice(ctx, 999, "x", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown device error = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteManagedDeviceRemovesPositionsAndUnregistersTracker(t *testing.T) {
	ctx := t.Context()
	store := openTestStore(t)
	device, err := store.CreateManagedDeviceWithTracker(ctx, "Auto", "st901-4g", "48600100200", "8160528336")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLocation(ctx, time.Now(), testFrame(t, "8160528336")); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSMSCommand(ctx, device.ID, "verify", "msg-1", "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteManagedDevice(ctx, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ManagedDevice(ctx, device.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("device still exists: %v", err)
	}
	if count, _ := store.CountPositions(ctx); count != 0 {
		t.Fatalf("positions kept after delete: %d", count)
	}
	if err := store.SaveLocation(ctx, time.Now(), testFrame(t, "8160528336")); !errors.Is(err, ErrUnknownTracker) {
		t.Fatalf("deleted tracker still accepted: %v", err)
	}
	// The phone number is free again.
	if _, err := store.CreateManagedDevice(ctx, "Auto", "st901-4g", "48600100200"); err != nil {
		t.Fatalf("re-adding the phone number: %v", err)
	}
	if err := store.DeleteManagedDevice(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown device error = %v, want sql.ErrNoRows", err)
	}
}
