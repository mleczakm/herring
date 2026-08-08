package sinotrack_test

import (
	"errors"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func TestParseLocation(t *testing.T) {
	frame := "*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#"

	got, err := sinotrack.ParseLocation(frame)
	if err != nil {
		t.Fatalf("ParseLocation() error = %v", err)
	}

	wantTime := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if got.DeviceID != "1234567890" || got.MessageType != "V1" {
		t.Errorf("unexpected identity: %#v", got)
	}
	if !got.TrackerTime.Equal(wantTime) {
		t.Errorf("TrackerTime = %v, want %v", got.TrackerTime, wantTime)
	}
	if got.Latitude != 52.218723 || got.Longitude != 21.009463 {
		t.Errorf("coordinates = (%f, %f)", got.Latitude, got.Longitude)
	}
	if got.SpeedKPH != 18.52 || got.Heading != 90 || !got.GPSValid {
		t.Errorf("unexpected movement fields: %#v", got)
	}
	if got.MCC != 260 || got.MNC != 6 || got.Status != "FFFFFFFF" {
		t.Errorf("unexpected network/status fields: %#v", got)
	}

	wantAck := "*HQ,1234567890,V4,V1,20260808120001#"
	if ack := got.Acknowledgement(wantTime.Add(time.Second)); ack != wantAck {
		t.Errorf("Acknowledgement() = %q, want %q", ack, wantAck)
	}
}

func TestParseLocationSouthernAndWesternHemispheres(t *testing.T) {
	frame := "*HQ,1234567890,V5,010203,V,3351.0000,S,15112.0000,W,000.00,0,010226,00000000,505,01#"

	got, err := sinotrack.ParseLocation(frame)
	if err != nil {
		t.Fatalf("ParseLocation() error = %v", err)
	}
	if got.Latitude != -33.85 || got.Longitude != -151.2 || got.GPSValid {
		t.Errorf("unexpected location: %#v", got)
	}
}

func TestParseLocationRejectsInvalidFrames(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		err   error
	}{
		{"missing delimiters", "HQ,1,V1", sinotrack.ErrMalformedFrame},
		{"too few fields", "*HQ,1,V1#", sinotrack.ErrMalformedFrame},
		{"command response", "*HQ,1,V4,OK#", sinotrack.ErrUnsupportedMessage},
		{"bad latitude", "*HQ,1,V1,120000,A,5261.0000,N,02100.0000,E,0.00,0,080826,0,260,06#", sinotrack.ErrMalformedFrame},
		{"bad heading", "*HQ,1,V1,120000,A,5200.0000,N,02100.0000,E,0.00,360,080826,0,260,06#", sinotrack.ErrMalformedFrame},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := sinotrack.ParseLocation(test.frame)
			if !errors.Is(err, test.err) {
				t.Fatalf("ParseLocation() error = %v, want %v", err, test.err)
			}
		})
	}
}

func FuzzParseLocation(f *testing.F) {
	f.Add("*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#")
	f.Add("")
	f.Fuzz(func(t *testing.T, frame string) {
		_, _ = sinotrack.ParseLocation(frame)
	})
}
