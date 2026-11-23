package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// unknown_trackers remembers tracker IDs whose frames were rejected because
// they are not registered, so the administrator can pick one when adding a
// tracker instead of searching the logs. It stores IDs and times only, never
// coordinates. The tracker port is public, so anything may appear here: an ID
// is only accepted after the administrator assigns it to a device.
const unknownTrackersSchema = `
CREATE TABLE IF NOT EXISTS unknown_trackers (
    tracker_id TEXT PRIMARY KEY,
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 1
) STRICT;

CREATE INDEX IF NOT EXISTS unknown_trackers_last_seen_idx
    ON unknown_trackers (last_seen_at);
`

// Unknown trackers are kept for a week, and only the most recent ones, so a
// flood of made-up IDs cannot grow the table.
const (
	UnknownTrackerWindow = 7 * 24 * time.Hour
	maxUnknownTrackers   = 20
)

var (
	// ErrInvalidTrackerID means a tracker ID is not 5 to 20 digits.
	ErrInvalidTrackerID = errors.New("tracker ID must be 5 to 20 digits")
	// ErrTrackerInUse means another managed device already uses the tracker ID.
	ErrTrackerInUse = errors.New("tracker ID belongs to another device")
	// ErrPhoneInUse means another managed device already uses the phone number.
	ErrPhoneInUse = errors.New("phone number belongs to another device")
	// ErrUnknownTracker means a frame came from a tracker ID that is not
	// registered, so its position was rejected.
	ErrUnknownTracker = errors.New("tracker ID is not registered")
)

// NormalizeTrackerID removes spaces from an ID typed by hand and checks that
// what is left looks like a tracker ID. ST-901 IDs are 10 digits; other H02
// trackers use up to the full 15-digit IMEI.
func NormalizeTrackerID(value string) (string, error) {
	id := strings.Join(strings.Fields(value), "")
	if !validTrackerID(id) {
		return "", ErrInvalidTrackerID
	}
	return id, nil
}

func validTrackerID(id string) bool {
	if len(id) < 5 || len(id) > 20 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// UnknownTracker is a tracker ID that recently tried to report in.
type UnknownTracker struct {
	ID                  string
	FirstSeen, LastSeen time.Time
	Attempts            int64
}

// RecordUnknownTracker notes a rejected frame from trackerID. IDs that are not
// plain digits are ignored: no real tracker sends them, so they could never be
// picked anyway.
func (s *Store) RecordUnknownTracker(ctx context.Context, trackerID string, now time.Time) error {
	if !validTrackerID(trackerID) {
		return nil
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO unknown_trackers (tracker_id, first_seen_at, last_seen_at)
        SELECT ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM devices WHERE id = ?)
        ON CONFLICT (tracker_id) DO UPDATE SET last_seen_at = excluded.last_seen_at, attempts = attempts + 1`,
		trackerID, formatTime(now), formatTime(now), trackerID); err != nil {
		return fmt.Errorf("record unknown tracker: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
        DELETE FROM unknown_trackers
        WHERE last_seen_at < ?
           OR tracker_id NOT IN (SELECT tracker_id FROM unknown_trackers ORDER BY last_seen_at DESC LIMIT ?)`,
		formatTime(now.Add(-UnknownTrackerWindow)), maxUnknownTrackers); err != nil {
		return fmt.Errorf("prune unknown trackers: %w", err)
	}
	return tx.Commit()
}

// UnknownTrackers lists tracker IDs rejected in the last week, most recent
// first, leaving out any that have been registered since.
func (s *Store) UnknownTrackers(ctx context.Context, now time.Time) ([]UnknownTracker, error) {
	rows, err := s.database.QueryContext(ctx, `
        SELECT tracker_id, first_seen_at, last_seen_at, attempts FROM unknown_trackers
        WHERE last_seen_at >= ? AND tracker_id NOT IN (SELECT id FROM devices)
        ORDER BY last_seen_at DESC LIMIT ?`, formatTime(now.Add(-UnknownTrackerWindow)), maxUnknownTrackers)
	if err != nil {
		return nil, fmt.Errorf("query unknown trackers: %w", err)
	}
	defer rows.Close()
	var out []UnknownTracker
	for rows.Next() {
		var (
			t           UnknownTracker
			first, last string
		)
		if err := rows.Scan(&t.ID, &first, &last, &t.Attempts); err != nil {
			return nil, err
		}
		t.FirstSeen, _ = time.Parse(time.RFC3339Nano, first)
		t.LastSeen, _ = time.Parse(time.RFC3339Nano, last)
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateManagedDeviceWithTracker adds a device and, when trackerID is not
// empty, registers that ID and links it to the device in one transaction, so
// its positions are accepted from the first frame.
func (s *Store) CreateManagedDeviceWithTracker(ctx context.Context, name, model, phone, trackerID string) (ManagedDevice, error) {
	if trackerID != "" && !validTrackerID(trackerID) {
		return ManagedDevice{}, ErrInvalidTrackerID
	}
	name = strings.TrimSpace(name)
	now := time.Now()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return ManagedDevice{}, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_devices WHERE phone_number = ?)`, phone).Scan(&exists); err != nil {
		return ManagedDevice{}, err
	}
	if exists {
		return ManagedDevice{}, ErrPhoneInUse
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO managed_devices(name,model,phone_number,config_status,config_started_at) VALUES(?,?,?,'sending',?)`, name, model, phone, formatTime(now))
	if err != nil {
		return ManagedDevice{}, fmt.Errorf("create managed device: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ManagedDevice{}, err
	}
	if trackerID != "" {
		if err := assignTracker(ctx, tx, id, trackerID, now); err != nil {
			return ManagedDevice{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ManagedDevice{}, err
	}
	return ManagedDevice{ID: id, Name: name, Model: model, PhoneNumber: phone, TrackerID: trackerID, ConfigStatus: "sending", ConfigStartedAt: now}, nil
}

// UpdateManagedDevice renames a device and, when trackerID is not empty, links
// it to that tracker ID. Changing the ID of a linked device unregisters the
// old ID, which deletes the positions stored under it: they came from a
// tracker that is no longer this device. It returns sql.ErrNoRows for an
// unknown device.
func (s *Store) UpdateManagedDevice(ctx context.Context, id int64, name, trackerID string) error {
	if trackerID != "" && !validTrackerID(trackerID) {
		return ErrInvalidTrackerID
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT tracker_id FROM managed_devices WHERE id = ?`, id).Scan(&current); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE managed_devices SET name = ? WHERE id = ?`, strings.TrimSpace(name), id); err != nil {
		return fmt.Errorf("rename managed device: %w", err)
	}
	if trackerID != "" && trackerID != current.String {
		if err := assignTracker(ctx, tx, id, trackerID, time.Now()); err != nil {
			return err
		}
		if current.Valid {
			if err := unregisterTracker(ctx, tx, current.String); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM device_contact WHERE device_id = ?`, id); err != nil {
				return fmt.Errorf("reset device contact: %w", err)
			}
		}
	}
	return tx.Commit()
}

// assignTracker registers trackerID and links it to the managed device.
func assignTracker(ctx context.Context, tx *sql.Tx, deviceID int64, trackerID string, now time.Time) error {
	var taken bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_devices WHERE tracker_id = ? AND id <> ?)`, trackerID, deviceID).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return ErrTrackerInUse
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices (id, created_at) VALUES (?, ?) ON CONFLICT (id) DO NOTHING`, trackerID, formatTime(now)); err != nil {
		return fmt.Errorf("register tracker: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE managed_devices SET tracker_id = ? WHERE id = ?`, trackerID, deviceID); err != nil {
		return fmt.Errorf("link tracker: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM unknown_trackers WHERE tracker_id = ?`, trackerID); err != nil {
		return fmt.Errorf("forget unknown tracker: %w", err)
	}
	return nil
}

// DeleteManagedDevice removes a device with its SMS history and alert
// settings. Its tracker ID is unregistered too, which deletes every position
// stored under it and makes the tracker's next frames rejected again. It
// returns sql.ErrNoRows for an unknown device.
func (s *Store) DeleteManagedDevice(ctx context.Context, id int64) error {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tracker sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT tracker_id FROM managed_devices WHERE id = ?`, id).Scan(&tracker); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM managed_devices WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete managed device: %w", err)
	}
	if tracker.Valid {
		if err := unregisterTracker(ctx, tx, tracker.String); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// unregisterTracker removes a tracker ID and its positions. The foreign key
// cascades too; deleting positions explicitly does not depend on the pragma.
func unregisterTracker(ctx context.Context, tx *sql.Tx, trackerID string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM positions WHERE device_id = ?`, trackerID); err != nil {
		return fmt.Errorf("delete tracker positions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, trackerID); err != nil {
		return fmt.Errorf("unregister tracker: %w", err)
	}
	return nil
}

// trackerRegistered reports whether trackerID may send positions.
func (s *Store) trackerRegistered(ctx context.Context, trackerID string) (bool, error) {
	var exists bool
	err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id = ?)`, trackerID).Scan(&exists)
	return exists, err
}
