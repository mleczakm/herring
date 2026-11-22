package st901

import (
	"math"
	"testing"
	"time"
)

func TestLocateCommand(t *testing.T) {
	command, err := Profile{}.LocateCommand()
	if err != nil || command.Body != "6690000" || command.Kind != "locate" {
		t.Fatalf("default locate command = %+v, %v", command, err)
	}
	command, _ = Profile{Password: "1234"}.LocateCommand()
	if command.Body != "6691234" {
		t.Fatalf("custom password command = %q", command.Body)
	}
	if _, err := (Profile{Password: "12a4"}).LocateCommand(); err == nil {
		t.Fatal("accepted a non-numeric password")
	}
}

func TestParseLocationReply(t *testing.T) {
	received := time.Date(2016, 5, 18, 13, 41, 10, 0, time.UTC)
	cases := []struct {
		name, body      string
		lat, lon, speed float64
		valid           bool
		id              string
		trackerTime     time.Time
		battery         int
	}{
		{
			// Example reply from the ST-901L manual.
			name: "manual example", body: "http://maps.google.com/maps?q=+22.64208,+114.18828 Date: 2016-5-18 Time:13:40 ID: 8160528336 Fix:A Speed:0KM/H Bat:5",
			lat: 22.64208, lon: 114.18828, valid: true, id: "8160528336", trackerTime: time.Date(2016, 5, 18, 13, 40, 0, 0, time.UTC), battery: 5,
		},
		{
			name: "southern and western link without fix", body: "http://maps.google.com/maps?q=-33.8688,-151.2093 Speed:42.5KM/H Fix:V",
			lat: -33.8688, lon: -151.2093, speed: 42.5, valid: false, trackerTime: received.Truncate(time.Minute), battery: -1,
		},
		{
			name: "hemisphere letters in link", body: "Position: https://maps.google.com/maps?q=N52.21872,E21.00946 Time:13:39:05 Date:2016-05-18",
			lat: 52.21872, lon: 21.00946, valid: true, trackerTime: time.Date(2016, 5, 18, 13, 39, 5, 0, time.UTC), battery: -1,
		},
		{
			name: "lat lon fields", body: "Lat:N52.218723 Lon:E21.009463 Spd:12km/h",
			lat: 52.218723, lon: 21.009463, speed: 12, valid: true, trackerTime: received.Truncate(time.Minute), battery: -1,
		},
	}
	for _, c := range cases {
		reply, ok := ParseLocationReply(c.body, received)
		if !ok {
			t.Errorf("%s: not parsed", c.name)
			continue
		}
		if math.Abs(reply.Latitude-c.lat) > 1e-9 || math.Abs(reply.Longitude-c.lon) > 1e-9 || reply.SpeedKPH != c.speed || reply.GPSValid != c.valid ||
			reply.TrackerID != c.id || !reply.TrackerTime.Equal(c.trackerTime) || reply.Battery != c.battery {
			t.Errorf("%s: got %+v", c.name, reply)
		}
	}
}

func TestParseLocationReplyRejectsNonPositions(t *testing.T) {
	received := time.Now()
	var (
		reply LocationReply
		ok    bool
	)
	for _, body := range []string{
		"ID:8160528336; MODE:GPRS; APN:internet; IP:65.108.44.244:20115",
		"SET OK",
		"http://maps.google.com/maps?q=0.0,0.0",
		"http://maps.google.com/maps?q=95.1,10.0",
		"",
	} {
		if reply, ok := ParseLocationReply(body, received); ok {
			t.Errorf("parsed %q as %+v", body, reply)
		}
	}
	// A clock in another time zone (here 8 hours ahead) is not trusted.
	reply, ok = ParseLocationReply("http://maps.google.com/maps?q=52.1,21.0 Date: "+received.Add(8*time.Hour).Format("2006-1-2")+" Time:"+received.Add(8*time.Hour).Format("15:04"), received)
	if !ok || !reply.TrackerTime.Equal(received.UTC().Truncate(time.Minute)) {
		t.Errorf("time zone offset accepted: %+v", reply)
	}
	// A date far from the receive time is ignored rather than trusted.
	reply, ok = ParseLocationReply("http://maps.google.com/maps?q=52.1,21.0 Date: 2000-1-1 Time:10:00", received)
	if !ok || !reply.TrackerTime.Equal(received.UTC().Truncate(time.Minute)) {
		t.Errorf("stale date accepted: %+v", reply)
	}
}
