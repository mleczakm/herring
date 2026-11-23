package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func TestRoutePositionsPagesThroughValidFixes(t *testing.T) {
	store, err := Open(t.Context(), filepath.Join(t.TempDir(), "herring.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RegisterDevice(t.Context(), "1234567890"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.OldestRouteTime(t.Context(), "1234567890"); ok || err != nil {
		t.Fatalf("oldest without positions = %v, %v", ok, err)
	}
	for _, frame := range []string{
		"*HQ,1234567890,V1,235900,A,5213.1234,N,02100.5678,E,010.00,90,070826,FFFFFFFF,260,06#",
		"*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#",
		"*HQ,1234567890,V1,120030,V,5213.1234,N,02100.5678,E,000.00,0,080826,FFFFFFFF,260,06#",
		"*HQ,1234567890,V1,120100,A,5213.2234,N,02100.5678,E,030.00,90,080826,FFFFFFFF,260,06#",
		"*HQ,1234567890,V1,120200,A,5213.3234,N,02100.5678,E,040.00,90,080826,FFFFFFFF,260,06#",
	} {
		location, err := sinotrack.ParseLocation(frame)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveLocation(t.Context(), time.Now(), location); err != nil {
			t.Fatal(err)
		}
	}
	from := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	first, err := store.RoutePositions(t.Context(), "1234567890", from, to, nil, 2)
	if err != nil || len(first) != 2 || first[0].TrackerTime.Minute() != 0 || first[1].TrackerTime.Minute() != 1 {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	last := first[len(first)-1]
	rest, err := store.RoutePositions(t.Context(), "1234567890", from, to, &RouteCursor{TrackerTime: last.TrackerTime, ID: last.ID}, 2)
	if err != nil || len(rest) != 1 || rest[0].TrackerTime.Minute() != 2 {
		t.Fatalf("second page = %+v, %v", rest, err)
	}
	other, err := store.RoutePositions(t.Context(), "other", from, to, nil, 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("other tracker = %+v, %v", other, err)
	}
	oldest, ok, err := store.OldestRouteTime(t.Context(), "1234567890")
	if err != nil || !ok || !oldest.Equal(time.Date(2026, 8, 7, 23, 59, 0, 0, time.UTC)) {
		t.Fatalf("oldest = %v, %v, %v", oldest, ok, err)
	}
}
