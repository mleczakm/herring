package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func TestAlertsTransitionsPrivacyAndSelection(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateInitialAdmin(ctx, "a@b.test", "A", "hash"); err != nil {
		t.Fatal(err)
	}
	u, err := s.UserByEmail(ctx, "a@b.test")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.CreateManagedDevice(ctx, "Boat", "st901-2g", "123")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkTrackerIfUnambiguous(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreatePlace(ctx, u.ID, Place{Name: "Home", Latitude: 52, Longitude: 21, RadiusM: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreatePlace(ctx, u.ID, Place{Name: "Other", Latitude: 52, Longitude: 21, RadiusM: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDeviceAlerts(ctx, u.ID, d.ID, AlertSettings{Entry: true, Exit: true, SpeedKPH: 50, Humor: true, PlaceIDs: []int64{p.ID}}); err != nil {
		t.Fatal(err)
	}
	loc := sinotrack.Location{DeviceID: "1234567890", GPSValid: true, Latitude: 52, Longitude: 21, TrackerTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	check := func(want ...string) {
		t.Helper()
		ev, err := s.EvaluateAlerts(ctx, loc)
		if err != nil {
			t.Fatal(err)
		}
		if len(ev) != len(want) {
			t.Fatalf("events %+v want %v", ev, want)
		}
		for i, k := range want {
			if ev[i].Kind != k || !ev[i].Humor {
				t.Fatalf("event %+v want %s", ev[i], k)
			}
		}
	}
	check()
	loc.TrackerTime = loc.TrackerTime.Add(time.Second)
	loc.Latitude = 53
	loc.SpeedKPH = 60
	check("exit", "speed")
	check()
	loc.TrackerTime = loc.TrackerTime.Add(-time.Second)
	loc.Latitude = 52
	loc.SpeedKPH = 0
	check()
	loc.TrackerTime = loc.TrackerTime.Add(2 * time.Second)
	loc.SpeedKPH = 0
	check("entry")
	loc.TrackerTime = loc.TrackerTime.Add(time.Second)
	loc.SpeedKPH = 60
	check("speed")
	if err = s.EnablePrivacy(ctx, "hash", "browser", time.Now()); err != nil {
		t.Fatal(err)
	}
	loc.TrackerTime = loc.TrackerTime.Add(time.Second)
	loc.Latitude = 53
	check()
	if err = s.DisablePrivacy(ctx); err != nil {
		t.Fatal(err)
	}
	loc.TrackerTime = loc.TrackerTime.Add(time.Second)
	check()
}
func TestPlaceSuggestionsRetentionAndSavedPlaces(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "suggest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateInitialAdmin(ctx, "a@b.test", "A", "hash"); err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByEmail(ctx, "a@b.test")
	if err = s.RegisterDevice(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateManagedDevice(ctx, "Auto", "st901-2g", "123"); err != nil {
		t.Fatal(err)
	}
	if err = s.LinkTrackerIfUnambiguous(ctx, "1234567890"); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveRetention(ctx, 3, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	for _, day := range []int{1, 2, 10} {
		loc := sinotrack.Location{DeviceID: "1234567890", GPSValid: true, Latitude: 52, Longitude: 21, TrackerTime: now.AddDate(0, 0, -day), Raw: time.Duration(day).String()}
		if err = s.SaveLocation(ctx, now.AddDate(0, 0, -day), loc); err != nil {
			t.Fatal(err)
		}
		loc.TrackerTime = loc.TrackerTime.Add(10 * time.Minute)
		loc.Raw += "second"
		if err = s.SaveLocation(ctx, now.AddDate(0, 0, -day), loc); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.PlaceSuggestions(ctx, u.ID, now)
	if err != nil || len(got) != 1 || got[0].Visits != 2 {
		t.Fatalf("suggestions %+v %v", got, err)
	}
	if _, err = s.CreatePlace(ctx, u.ID, Place{Name: "Home", Latitude: 52, Longitude: 21, RadiusM: 100}); err != nil {
		t.Fatal(err)
	}
	got, err = s.PlaceSuggestions(ctx, u.ID, now)
	if err != nil || len(got) != 0 {
		t.Fatalf("saved suggestions %+v %v", got, err)
	}
}
