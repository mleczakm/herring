package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mleczakm/herring/internal/storage/sqlite"
	"github.com/mleczakm/herring/internal/tracker/st901"
)

// Asking for a position by SMS costs one outgoing SMS from the gateway and one
// reply from the tracker's SIM, so requests per device are spaced out.
const locateInterval = 2 * time.Minute

var (
	// ErrSMSUnavailable means the SMS gateway is not configured.
	ErrSMSUnavailable = errors.New("SMS gateway is not configured")
	// ErrLocateTooSoon means a position was requested by SMS moments ago.
	ErrLocateTooSoon = errors.New("position was requested by SMS recently")
)

// RequestSMSLocation sends the ST-901 "669" command, which makes the tracker
// answer with its position by SMS. The reply arrives through the SMS webhook.
// It works when the tracker has no mobile data but still has GSM coverage.
func (s *Server) RequestSMSLocation(ctx context.Context, deviceID int64) error {
	if s.sender == nil {
		return ErrSMSUnavailable
	}
	device, err := s.store.ManagedDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	last, ok, err := s.store.LastSMSCommand(ctx, deviceID, "locate")
	if err != nil {
		return err
	}
	if ok && time.Since(last) < locateInterval {
		return ErrLocateTooSoon
	}
	command, err := s.config.Tracker.LocateCommand()
	if err != nil {
		return err
	}
	messageID, err := s.sender.Send(ctx, device.PhoneNumber, command.Body)
	if err != nil {
		return err
	}
	return s.store.RecordSMSCommand(ctx, deviceID, command.Kind, messageID, "accepted")
}

func (s *Server) locate(w http.ResponseWriter, r *http.Request) {
	if !s.jsonWrite(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		jsonError(w, 400, "invalid_id")
		return
	}
	switch err := s.RequestSMSLocation(r.Context(), id); {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, sql.ErrNoRows):
		jsonError(w, 404, "not_found")
	case errors.Is(err, ErrSMSUnavailable):
		jsonError(w, 503, "sms_unavailable")
	case errors.Is(err, ErrLocateTooSoon):
		w.Header().Set("Retry-After", strconv.Itoa(int(locateInterval.Seconds())))
		jsonError(w, 429, "too_soon")
	default:
		s.logger.Error("SMS location request failed", "device_id", id, "error", err)
		jsonError(w, 502, "sms_failed")
	}
}

// handleLocationReply stores a position the tracker sent by SMS. It reports
// whether the message was a location reply at all.
func (s *Server) handleLocationReply(ctx context.Context, device sqlite.ManagedDevice, body string) bool {
	// Configuration replies (RCONF) are handled by the caller.
	if strings.Contains(strings.ToUpper(body), "MODE:") {
		return false
	}
	now := time.Now()
	reply, ok := st901.ParseLocationReply(body, now)
	if !ok {
		return false
	}
	inserted, err := s.store.SaveSMSPosition(ctx, device.ID, sqlite.SMSPosition{
		TrackerID: reply.TrackerID, Latitude: reply.Latitude, Longitude: reply.Longitude, SpeedKPH: reply.SpeedKPH,
		GPSValid: reply.GPSValid, TrackerTime: reply.TrackerTime, Raw: body,
	}, now)
	switch {
	case errors.Is(err, sqlite.ErrTrackerNotLinked):
		s.logger.Warn("SMS position for a device without a linked tracker", "device_id", device.ID)
		return true
	case err != nil:
		s.logger.Error("could not store SMS position", "device_id", device.ID, "error", err)
		return true
	case !inserted:
		// A repeated webhook delivery of the same SMS.
		return true
	}
	s.logger.Info("SMS position received", "device_id", device.ID)
	if s.config.Push != nil {
		events, err := s.store.SMSPositionEvents(ctx, device.ID)
		if err != nil {
			s.logger.Warn("could not load SMS position alerts", "error", err)
			return true
		}
		// Deliver outside the webhook request so the gateway gets its answer
		// quickly; Send bounds itself with its own deadline.
		go func() {
			for _, event := range events {
				if err := s.config.Push.Send(context.Background(), event); err != nil {
					s.logger.Warn("could not deliver SMS position alert", "error", err)
				}
			}
		}()
	}
	return true
}
