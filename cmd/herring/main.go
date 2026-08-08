package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mleczakm/herring/internal/httpapi"
	"github.com/mleczakm/herring/internal/ingest"
	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpAddress := environment("HERRING_HTTP_ADDR", ":8080")
	trackerAddress := environment("HERRING_TRACKER_ADDR", ":8090")
	databasePath := environment("HERRING_DATABASE_PATH", "herring.db")
	setupToken := os.Getenv("HERRING_SETUP_TOKEN")
	if os.Getenv("HERRING_ENV") == "production" && setupToken == "" {
		logger.Error("HERRING_SETUP_TOKEN is required in production")
		os.Exit(1)
	}
	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		logger.Error("could not open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	for _, deviceID := range configuredDeviceIDs(os.Getenv("HERRING_DEVICE_IDS")) {
		if err := store.RegisterDevice(ctx, deviceID); err != nil {
			logger.Error("could not register configured device", "device_id", deviceID, "error", err)
			os.Exit(1)
		}
	}

	httpHandler := httpapi.New(store, setupToken, logger)
	httpServer := &http.Server{
		Addr:              httpAddress,
		Handler:           httpHandler.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	trackerServer := &ingest.Server{
		Addr:   trackerAddress,
		Logger: logger,
		OnLocation: func(ctx context.Context, location sinotrack.Location) error {
			receivedAt := time.Now()
			if err := store.SaveLocation(ctx, receivedAt, location); err != nil {
				return err
			}
			// Never log raw frames because future variants can contain sensitive data.
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

func configuredDeviceIDs(value string) []string {
	var result []string
	for _, deviceID := range strings.Split(value, ",") {
		if deviceID = strings.TrimSpace(deviceID); deviceID != "" {
			result = append(result, deviceID)
		}
	}
	return result
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
