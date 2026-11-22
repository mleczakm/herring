package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	"github.com/mleczakm/herring/internal/push"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

// Delivery runs outside the TCP handler. A slow or offline notification
// provider must not delay a tracker's acknowledgement or the next GPS fix.
type notifications struct {
	store   *sqlite.Store
	service *push.Service
	queue   chan sqlite.AlertEvent
	logger  *slog.Logger
}

func startNotifications(ctx context.Context, store *sqlite.Store, logger *slog.Logger) (*notifications, error) {
	service, err := push.New(store, push.Config{
		PublicKey:   os.Getenv("HERRING_VAPID_PUBLIC_KEY"),
		PrivateKey:  os.Getenv("HERRING_VAPID_PRIVATE_KEY"),
		Subject:     os.Getenv("HERRING_VAPID_SUBJECT"),
		GeminiKey:   environment("HERRING_GEMINI_API_KEY", os.Getenv("GEMINI_API_KEY")),
		GeminiModel: os.Getenv("HERRING_GEMINI_MODEL"),
	})
	if err != nil {
		return nil, err
	}
	n := &notifications{store: store, service: service, queue: make(chan sqlite.AlertEvent, 64), logger: logger}
	go n.deliver(ctx)
	return n, nil
}

func (n *notifications) observe(ctx context.Context, fix sinotrack.Location) {
	events, err := n.store.EvaluateAlerts(ctx, fix)
	if err != nil {
		n.logger.Warn("could not evaluate tracking alerts", "error", err)
		return
	}
	for _, event := range events {
		select {
		case n.queue <- event:
		default:
			n.logger.Warn("notification queue full")
			return
		}
	}
}

func (n *notifications) deliver(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-n.queue:
			err := n.service.Send(ctx, event)
			if err != nil && ctx.Err() == nil {
				n.logger.Warn("could not deliver tracking alert", "error", err)
			}
		}
	}
}
