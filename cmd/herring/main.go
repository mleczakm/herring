package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mleczakm/herring/internal/httpapi"
	"github.com/mleczakm/herring/internal/ingest"
	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	"github.com/mleczakm/herring/internal/sms/sendly"
	"github.com/mleczakm/herring/internal/storage/sqlite"
	"github.com/mleczakm/herring/internal/tracker/st901"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpAddress := environment("HERRING_HTTP_ADDR", ":8080")
	trackerAddress := environment("HERRING_TRACKER_ADDR", ":8090")
	databasePath := environment("HERRING_DATABASE_PATH", "herring.db")
	setupToken := os.Getenv("HERRING_SETUP_TOKEN")
	store, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		logger.Error("could not open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	if os.Getenv("HERRING_ENV") == "production" && setupToken == "" {
		setupRequired, err := store.SetupRequired(ctx)
		if err != nil {
			logger.Error("could not determine setup status", "error", err)
			os.Exit(1)
		}
		if setupRequired {
			logger.Error("HERRING_SETUP_TOKEN is required for initial production setup")
			os.Exit(1)
		}
	}
	for _, deviceID := range configuredDeviceIDs(os.Getenv("HERRING_DEVICE_IDS")) {
		if err := store.RegisterDevice(ctx, deviceID); err != nil {
			logger.Error("could not register configured device", "device_id", deviceID, "error", err)
			os.Exit(1)
		}
	}

	var smsSender httpapi.Sender
	if os.Getenv("HERRING_SENDLY_TOKEN") != "" && os.Getenv("HERRING_SENDLY_FROM") != "" {
		smsSender = &sendly.Client{Token: os.Getenv("HERRING_SENDLY_TOKEN"), From: os.Getenv("HERRING_SENDLY_FROM")}
	}
	trackerPort, err := strconv.Atoi(environment("HERRING_TRACKER_PUBLIC_PORT", "20115"))
	if err != nil {
		logger.Error("invalid tracker public port")
		os.Exit(1)
	}
	movingInterval, err := strconv.Atoi(environment("HERRING_TRACKER_MOVING_INTERVAL", "20"))
	if err != nil {
		logger.Error("invalid moving interval")
		os.Exit(1)
	}
	stoppedInterval, err := strconv.Atoi(environment("HERRING_TRACKER_STOPPED_INTERVAL", "300"))
	if err != nil {
		logger.Error("invalid stopped interval")
		os.Exit(1)
	}
	httpHandler := httpapi.New(store, smsSender, httpapi.Config{
		SetupToken: setupToken, WebhookSecret: os.Getenv("HERRING_SENDLY_WEBHOOK_SECRET"), SecureCookies: os.Getenv("HERRING_ENV") == "production",
		Tracker: st901.Profile{Password: environment("HERRING_TRACKER_PASSWORD", "0000"), ControlNumber: os.Getenv("HERRING_SENDLY_FROM"), APN: os.Getenv("HERRING_TRACKER_APN"), ServerHost: os.Getenv("HERRING_TRACKER_PUBLIC_HOST"), ServerPort: trackerPort, MovingInterval: movingInterval, StoppedInterval: stoppedInterval},
	}, logger)
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
