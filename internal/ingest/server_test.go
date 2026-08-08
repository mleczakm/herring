package ingest

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func TestHandleConnectionDecodesAndAcknowledgesFrame(t *testing.T) {
	serverConnection, trackerConnection := net.Pipe()
	locations := make(chan sinotrack.Location, 1)
	server := Server{
		IdleTimeout: time.Second,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnLocation: func(_ context.Context, location sinotrack.Location) error {
			locations <- location
			return nil
		},
	}
	go server.handleConnection(context.Background(), serverConnection)
	t.Cleanup(func() { _ = trackerConnection.Close() })

	frame := "*HQ,1234567890,V1,120000,A,5213.1234,N,02100.5678,E,010.00,90,080826,FFFFFFFF,260,06#"
	if _, err := io.WriteString(trackerConnection, frame[:20]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(trackerConnection, frame[20:]); err != nil {
		t.Fatal(err)
	}

	select {
	case location := <-locations:
		if location.DeviceID != "1234567890" {
			t.Errorf("DeviceID = %q", location.DeviceID)
		}
	case <-time.After(time.Second):
		t.Fatal("location was not handled")
	}

	response := make([]byte, 128)
	n, err := trackerConnection.Read(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(response[:n]); !strings.HasPrefix(got, "*HQ,1234567890,V4,V1,") || !strings.HasSuffix(got, "#") {
		t.Errorf("unexpected acknowledgement %q", got)
	}
}

func TestHandleConnectionRejectsOversizedFrame(t *testing.T) {
	serverConnection, trackerConnection := net.Pipe()
	server := Server{
		MaxFrameSize: 16,
		IdleTimeout:  time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan struct{})
	go func() {
		server.handleConnection(context.Background(), serverConnection)
		close(done)
	}()

	_, _ = io.WriteString(trackerConnection, "*"+strings.Repeat("A", 32)+"#")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("oversized connection was not closed")
	}
	_ = trackerConnection.Close()
}
