package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/mleczakm/herring/internal/httpapi"
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
	guard, guardErr := n.store.EvaluateGuard(ctx, fix, time.Now())
	if guardErr != nil {
		n.logger.Warn("could not evaluate vehicle guard", "error", guardErr)
	}
	if guard != nil {
		n.enqueue(*guard)
	}
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

func (n *notifications) enqueue(event sqlite.AlertEvent) {
	select {
	case n.queue <- event:
	default:
		n.logger.Warn("notification queue full")
	}
}

// contact records a GPRS frame and announces recovery after an outage.
func (n *notifications) contact(ctx context.Context, trackerID string, now time.Time) {
	events, err := n.store.TouchTracker(ctx, trackerID, now)
	if err != nil {
		n.logger.Warn("could not record tracker contact", "error", err)
		return
	}
	for _, event := range events {
		n.enqueue(event)
	}
}

// locator sends the SMS position request; *httpapi.Server implements it.
type locator interface {
	RequestSMSLocation(context.Context, int64) error
}

// watchOffline reports devices that stopped sending positions. When a user
// enabled the SMS fallback, it asks the tracker for its position by SMS once
// per outage, which works without mobile data as long as GSM is available.
func (n *notifications) watchOffline(ctx context.Context, sms locator) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		events, err := n.store.OfflineEvents(ctx, time.Now())
		if err != nil {
			if ctx.Err() == nil {
				n.logger.Warn("could not check for offline trackers", "error", err)
			}
			continue
		}
		requested := map[int64]bool{}
		for _, event := range events {
			if event.SMSFallback {
				sent, done := requested[event.DeviceID]
				if !done {
					err := sms.RequestSMSLocation(ctx, event.DeviceID)
					// A request made moments ago still counts as asked.
					sent = err == nil || errors.Is(err, httpapi.ErrLocateTooSoon)
					if err != nil && !sent {
						n.logger.Warn("could not request SMS position", "device_id", event.DeviceID, "error", err)
					}
					requested[event.DeviceID] = sent
				}
				event.SMSRequested = sent
			}
			n.logger.Info("tracker offline", "device_id", event.DeviceID, "silent_for", event.OfflineFor.Round(time.Minute).String())
			n.enqueue(event)
		}
	}
}

func (n *notifications) watchVehicles(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	check := func() {
		now := time.Now()
		for _, load := range []func(context.Context, time.Time) ([]sqlite.ScheduledVehicleEvent, error){n.store.DueReminders, n.store.MonthlyEvents} {
			events, err := load(ctx, now)
			if err != nil && ctx.Err() == nil {
				n.logger.Warn("could not check vehicle notifications", "error", err)
			}
			for _, event := range events {
				if ctx.Err() != nil {
					return
				}
				err := n.service.Send(ctx, event.AlertEvent)
				if err == nil {
					err = n.store.MarkVehicleEventSent(ctx, event)
				}
				if err != nil && ctx.Err() == nil {
					n.logger.Warn("could not send scheduled vehicle notification", "kind", event.Kind, "error", err)
				}
			}
		}
	}
	for {
		check()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
