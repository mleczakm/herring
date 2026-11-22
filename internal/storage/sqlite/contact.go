package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// device_contact records when each managed device last reached Herring over
// GPRS. It is kept apart from positions because privacy mode and retention
// delete positions, while offline detection still needs the last contact.
// It stores times only, never coordinates.
const contactSchema = `
CREATE TABLE IF NOT EXISTS device_contact (
    device_id INTEGER PRIMARY KEY REFERENCES managed_devices(id) ON DELETE CASCADE,
    last_seen_at TEXT NOT NULL,
    offline_notified_at TEXT
) STRICT;
`

// Offline alerts accept 10 minutes to 7 days; 0 turns them off.
const (
	MinOfflineMinutes = 10
	MaxOfflineMinutes = 7 * 24 * 60
)

// ErrTrackerNotLinked means an SMS position arrived for a device whose tracker
// ID is still unknown, so it cannot be stored with the device's history.
var ErrTrackerNotLinked = errors.New("tracker is not linked to the device yet")

// InitContact creates the contact table and adds the columns introduced with
// offline alerts and SMS positions to existing databases.
func (s *Store) InitContact(ctx context.Context) error {
	if _, err := s.database.ExecContext(ctx, contactSchema); err != nil {
		return fmt.Errorf("create contact table: %w", err)
	}
	for _, c := range []struct{ table, column, definition string }{
		{"alert_settings", "offline_minutes", "INTEGER NOT NULL DEFAULT 0"},
		{"alert_settings", "sms_fallback", "INTEGER NOT NULL DEFAULT 0"},
		{"positions", "source", "TEXT NOT NULL DEFAULT 'gprs'"},
	} {
		if err := s.addColumnIfMissing(ctx, c.table, c.column, c.definition); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing is a minimal migration: CREATE TABLE IF NOT EXISTS never
// changes an existing table, so new columns are added explicitly.
func (s *Store) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	rows, err := s.database.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// Identifiers come from the fixed list in InitContact, never from input.
	if _, err := s.database.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// TouchTracker records a GPRS frame from trackerID. When the device had been
// reported offline, it returns "online" events for users who asked for
// offline alerts.
func (s *Store) TouchTracker(ctx context.Context, trackerID string, now time.Time) ([]AlertEvent, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var (
		deviceID int64
		name     string
	)
	err = tx.QueryRowContext(ctx, `SELECT id, name FROM managed_devices WHERE tracker_id = ?`, trackerID).Scan(&deviceID, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var notified sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT offline_notified_at FROM device_contact WHERE device_id = ?`, deviceID).Scan(&notified)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
        INSERT INTO device_contact (device_id, last_seen_at, offline_notified_at) VALUES (?, ?, NULL)
        ON CONFLICT (device_id) DO UPDATE SET last_seen_at = excluded.last_seen_at, offline_notified_at = NULL`,
		deviceID, formatTime(now)); err != nil {
		return nil, err
	}
	var events []AlertEvent
	if notified.Valid {
		rows, err := tx.QueryContext(ctx, `SELECT user_id, humor FROM alert_settings WHERE device_id = ? AND offline_minutes > 0`, deviceID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			e := AlertEvent{DeviceID: deviceID, DeviceName: name, Kind: "online"}
			if err := rows.Scan(&e.UserID, &e.Humor); err != nil {
				rows.Close()
				return nil, err
			}
			events = append(events, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return events, tx.Commit()
}

// OfflineEvents finds devices silent for longer than a user's threshold,
// marks the outage as reported, and returns one "offline" event per user.
// Each outage is reported once; TouchTracker re-arms it.
func (s *Store) OfflineEvents(ctx context.Context, now time.Time) ([]AlertEvent, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
        SELECT a.user_id, m.id, m.name, a.offline_minutes, a.sms_fallback, a.humor, c.last_seen_at
        FROM alert_settings a
        JOIN managed_devices m ON m.id = a.device_id
        JOIN device_contact c ON c.device_id = m.id
        WHERE a.offline_minutes > 0 AND c.offline_notified_at IS NULL`)
	if err != nil {
		return nil, err
	}
	var events []AlertEvent
	for rows.Next() {
		var (
			e        AlertEvent
			minutes  int
			lastSeen string
		)
		if err := rows.Scan(&e.UserID, &e.DeviceID, &e.DeviceName, &minutes, &e.SMSFallback, &e.Humor, &lastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		seen, err := time.Parse(time.RFC3339Nano, lastSeen)
		if err != nil || now.Sub(seen) < time.Duration(minutes)*time.Minute {
			continue
		}
		e.Kind = "offline"
		e.OfflineFor = now.Sub(seen)
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		if _, err := tx.ExecContext(ctx, `UPDATE device_contact SET offline_notified_at = ? WHERE device_id = ?`, formatTime(now), e.DeviceID); err != nil {
			return nil, err
		}
	}
	return events, tx.Commit()
}

// LastContact reports when a managed device last sent a GPRS frame.
func (s *Store) LastContact(ctx context.Context, deviceID int64) (time.Time, bool, error) {
	var value string
	err := s.database.QueryRowContext(ctx, `SELECT last_seen_at FROM device_contact WHERE device_id = ?`, deviceID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil, err
}

// SMSPosition is a position the tracker sent back by SMS.
type SMSPosition struct {
	TrackerID                     string
	Latitude, Longitude, SpeedKPH float64
	GPSValid                      bool
	TrackerTime                   time.Time
	Raw                           string
}

// SaveSMSPosition stores an SMS position in the device's history. If the
// device has no tracker yet, the reply's ID links it, but only when that ID is
// allowlisted and unclaimed. It reports whether a new row was written, so a
// repeated webhook delivery is not mistaken for a new position.
func (s *Store) SaveSMSPosition(ctx context.Context, deviceID int64, p SMSPosition, receivedAt time.Time) (bool, error) {
	if !validCoordinates(p.Latitude, p.Longitude) || p.SpeedKPH < 0 {
		return false, errors.New("invalid SMS position")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var tracker sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT tracker_id FROM managed_devices WHERE id = ?`, deviceID).Scan(&tracker); err != nil {
		return false, err
	}
	trackerID := tracker.String
	if trackerID == "" && p.TrackerID != "" {
		result, err := tx.ExecContext(ctx, `
            UPDATE managed_devices SET tracker_id = ?
            WHERE id = ? AND tracker_id IS NULL
              AND EXISTS (SELECT 1 FROM devices WHERE id = ?)
              AND NOT EXISTS (SELECT 1 FROM managed_devices WHERE tracker_id = ?)`, p.TrackerID, deviceID, p.TrackerID, p.TrackerID)
		if err != nil {
			return false, err
		}
		if n, _ := result.RowsAffected(); n == 1 {
			trackerID = p.TrackerID
		}
	}
	if trackerID == "" {
		return false, ErrTrackerNotLinked
	}
	// The gateway retries webhooks; a late retry of the same SMS may get a
	// different fallback timestamp, so also match on content.
	var repeated bool
	if err := tx.QueryRowContext(ctx, `
        SELECT EXISTS (SELECT 1 FROM positions WHERE device_id = ? AND source = 'sms' AND raw_frame = ? AND received_at >= ?)`,
		trackerID, p.Raw, formatTime(receivedAt.Add(-time.Hour))).Scan(&repeated); err != nil {
		return false, err
	}
	if repeated {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `
        INSERT INTO positions (device_id, tracker_time, received_at, gps_valid, latitude, longitude, speed_kph, heading, status, mcc, mnc, raw_frame, source)
        VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'SMS', 0, 0, ?, 'sms')
        ON CONFLICT (device_id, tracker_time, raw_frame) DO NOTHING`,
		trackerID, formatTime(p.TrackerTime), formatTime(receivedAt), p.GPSValid, p.Latitude, p.Longitude, p.SpeedKPH, p.Raw)
	if err != nil {
		return false, fmt.Errorf("save SMS position: %w", err)
	}
	inserted, _ := result.RowsAffected()
	return inserted == 1, tx.Commit()
}

// SMSPositionEvents returns "sms_position" events for users who asked to be
// told about this device when it goes silent.
func (s *Store) SMSPositionEvents(ctx context.Context, deviceID int64) ([]AlertEvent, error) {
	rows, err := s.database.QueryContext(ctx, `
        SELECT a.user_id, m.name, a.humor FROM alert_settings a JOIN managed_devices m ON m.id = a.device_id
        WHERE a.device_id = ? AND (a.offline_minutes > 0 OR a.sms_fallback = 1)`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AlertEvent
	for rows.Next() {
		e := AlertEvent{DeviceID: deviceID, Kind: "sms_position"}
		if err := rows.Scan(&e.UserID, &e.DeviceName, &e.Humor); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// LastSMSCommand reports when a command of this kind was last sent to a device.
func (s *Store) LastSMSCommand(ctx context.Context, deviceID int64, kind string) (time.Time, bool, error) {
	var value sql.NullString
	if err := s.database.QueryRowContext(ctx, `SELECT MAX(created_at) FROM sms_commands WHERE managed_device_id = ? AND kind = ?`, deviceID, kind).Scan(&value); err != nil {
		return time.Time{}, false, err
	}
	if !value.Valid {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, value.String)
	return t, err == nil, err
}

// CountSMSCommandsSince counts SMS messages Herring sent to trackers.
func (s *Store) CountSMSCommandsSince(ctx context.Context, since time.Time) (int64, error) {
	var count int64
	err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sms_commands WHERE created_at >= ?`, formatTime(since)).Scan(&count)
	return count, err
}

// ManagedDevice returns one managed device.
func (s *Store) ManagedDevice(ctx context.Context, id int64) (ManagedDevice, error) {
	return scanManaged(s.database.QueryRowContext(ctx, `SELECT id,name,model,phone_number,COALESCE(tracker_id,''),config_status,config_detail,config_started_at,config_verified_at FROM managed_devices WHERE id=?`, id))
}
