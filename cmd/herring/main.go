package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mleczakm/herring/internal/ingest"
	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpAddress := environment("HERRING_HTTP_ADDR", ":8080")
	trackerAddress := environment("HERRING_TRACKER_ADDR", ":8090")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
	})
	httpServer := &http.Server{
		Addr:              httpAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	trackerServer := &ingest.Server{
		Addr:   trackerAddress,
		Logger: logger,
		OnLocation: func(_ context.Context, location sinotrack.Location) error {
			// Persistence is the next application boundary. Never log raw frames.
			logger.Info("tracker location received",
				"device_id", location.DeviceID,
				"tracker_time", location.TrackerTime,
				"gps_valid", location.GPSValid,
				"latitude", location.Latitude,
				"longitude", location.Longitude,
			)
			return nil
		},
	}

	errorsChannel := make(chan error, 2)
	go func() {
		logger.Info("HTTP server started", "address", httpAddress)
		err := httpServer.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errorsChannel <- err
		}
	}()
	go func() { errorsChannel <- trackerServer.ListenAndServe(ctx) }()

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-errorsChannel:
		if err != nil {
			logger.Error("server stopped", "error", err)
		}
		stop()
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		logger.Error("HTTP shutdown failed", "error", err)
	}
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
