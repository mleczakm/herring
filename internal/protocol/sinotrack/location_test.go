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

func TestParseLocationWithTraccarFixtures(t *testing.T) {
	// Source: traccar/traccar H02ProtocolDecoderTest.java at commit
	// 17e7a330e8a07896f000898b37dc770f2df3c142 (Apache-2.0).
	tests := []struct {
		name      string
		frame     string
		latitude  float64
		longitude float64
	}{
		{
			"V8 with longitude containing minutes only",
			"*HQ,9001000002,V8,213945,A,3542.2043,N,38.6508,W,0.00,170,221025,FBFFF9FF,0,0,0,0,22,31,126,0#",
			35.703405,
			-0.64418,
		},
		{
			"V1 without optional network fields",
			"*HQ,865205035331981,V1,132926,A,1935.3933,N,07920.4134,E,  3.34,342,280519,FFFFFFFF#",
			19.589888,
			79.340223,
		},
		{
			"V1 Polish coordinates and extended fields",
			"*HQ,4210209006,V1,054048,A,2828.2297,N,07733.4332,E,000.5,047,080918,EEE7FBDF,4261193,0#",
			28.470495,
			77.55722,
		},
		{
			"V1 hexadecimal network codes",
			"*HQ,353588020068342,V1,084436,A,3257.01525,N,00655.03865,W,57.78,40,011216,FFFBFFFF,25c,a,154,b04c#",
			32.950254,
			-6.917311,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := sinotrack.ParseLocation(test.frame)
			if err != nil {
				t.Fatalf("ParseLocation() error = %v", err)
			}
			if got.Latitude != test.latitude || got.Longitude != test.longitude {
				t.Errorf("coordinates = (%f, %f), want (%f, %f)", got.Latitude, got.Longitude, test.latitude, test.longitude)
			}
		})
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
