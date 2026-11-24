package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

const vehiclesSchema = `
CREATE TABLE IF NOT EXISTS vehicle_profiles (
	device_id INTEGER PRIMARY KEY REFERENCES managed_devices(id) ON DELETE CASCADE,
	kind TEXT NOT NULL DEFAULT 'other',
	icon TEXT NOT NULL DEFAULT 'auto',
	odometer_m REAL NOT NULL DEFAULT 0
) STRICT;
CREATE TABLE IF NOT EXISTS trip_notes (
	device_id INTEGER NOT NULL REFERENCES managed_devices(id) ON DELETE CASCADE,
	started TEXT NOT NULL,
	note TEXT NOT NULL,
	PRIMARY KEY(device_id,started)
) STRICT;
CREATE TABLE IF NOT EXISTS service_reminders (
	id INTEGER PRIMARY KEY,
	device_id INTEGER NOT NULL REFERENCES managed_devices(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	due_date TEXT NOT NULL,
	due_m REAL NOT NULL,
	notified INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE TABLE IF NOT EXISTS vehicle_guards (
	device_id INTEGER PRIMARY KEY REFERENCES managed_devices(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	lat REAL NOT NULL, lon REAL NOT NULL, radius REAL NOT NULL,
	last_fix TEXT NOT NULL,
	outside INTEGER NOT NULL DEFAULT 0,
	triggered INTEGER NOT NULL DEFAULT 0
) STRICT;
CREATE TABLE IF NOT EXISTS location_shares (
	token_hash TEXT PRIMARY KEY,
	device_id INTEGER NOT NULL REFERENCES managed_devices(id) ON DELETE CASCADE,
	expires INTEGER NOT NULL
) STRICT;
CREATE TABLE IF NOT EXISTS monthly_reports (
	user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	last_month TEXT NOT NULL
) STRICT;
CREATE TRIGGER IF NOT EXISTS vehicle_privacy_enabled AFTER UPDATE OF privacy_mode ON settings
WHEN NEW.privacy_mode=1
BEGIN
	DELETE FROM location_shares;
	DELETE FROM vehicle_guards;
END;
CREATE TRIGGER IF NOT EXISTS purge_trip_note AFTER DELETE ON positions
BEGIN
	DELETE FROM trip_notes WHERE device_id=(SELECT id FROM managed_devices WHERE tracker_id=OLD.device_id)
	AND started=strftime('%Y-%m-%dT%H:%M:%fZ',OLD.tracker_time);
END;
`

type VehicleProfile struct {
	Kind      string  `json:"vehicle_type"`
	Icon      string  `json:"map_icon"`
	OdometerM float64 `json:"-"`
}

func (s *Store) VehicleProfile(ctx context.Context, id int64) (VehicleProfile, error) {
	p := VehicleProfile{Kind: "other", Icon: "auto"}
	err := s.database.QueryRowContext(ctx, `SELECT kind,icon,odometer_m FROM vehicle_profiles WHERE device_id=?`, id).Scan(&p.Kind, &p.Icon, &p.OdometerM)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

func (s *Store) SaveVehicleProfile(ctx context.Context, id int64, kind, icon string, odometer *float64) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO vehicle_profiles(device_id,kind,icon,odometer_m)
	VALUES(?,?,?,coalesce(?,0)) ON CONFLICT(device_id) DO UPDATE SET
	kind=excluded.kind,icon=excluded.icon,odometer_m=coalesce(?,vehicle_profiles.odometer_m)`,
		id, kind, icon, odometer, odometer)
	return err
}

func updateOdometer(ctx context.Context, tx *sql.Tx, loc sinotrack.Location, now time.Time) error {
	if !loc.GPSValid || loc.TrackerTime.Before(now.Add(-tripGap)) || loc.TrackerTime.After(now.Add(time.Minute)) {
		return nil
	}
	var id int64
	var prev TrackPoint
	var stamp string
	err := tx.QueryRowContext(ctx, `
	SELECT d.id,p.tracker_time,p.latitude,p.longitude,p.speed_kph FROM managed_devices d
	JOIN positions p ON p.device_id=d.tracker_id
	WHERE d.tracker_id=? AND p.gps_valid=1 AND p.source='gprs'
	AND (SELECT privacy_mode FROM settings WHERE id=1)=0
	ORDER BY julianday(p.tracker_time) DESC,p.id DESC LIMIT 1`, loc.DeviceID).Scan(&id, &stamp, &prev.Lat, &prev.Lon, &prev.Speed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	prev.Time, _ = time.Parse(time.RFC3339Nano, stamp)
	d := SegmentDistance(prev, TrackPoint{loc.TrackerTime, loc.Latitude, loc.Longitude, loc.SpeedKPH})
	if d == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO vehicle_profiles(device_id,odometer_m) VALUES(?,?)
	ON CONFLICT(device_id) DO UPDATE SET odometer_m=odometer_m+excluded.odometer_m`, id, d)
	return err
}

const tripNoteTime = "2006-01-02T15:04:05.000Z"

func (s *Store) TripNotes(ctx context.Context, id int64) (map[string]string, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT started,note FROM trip_notes WHERE device_id=? AND (SELECT privacy_mode FROM settings WHERE id=1)=0`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var started, note string
		if err := rows.Scan(&started, &note); err != nil {
			return nil, err
		}
		out[started] = note
	}
	return out, rows.Err()
}
func (s *Store) SaveTripNote(ctx context.Context, id int64, start time.Time, note string) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO trip_notes(device_id,started,note)
	SELECT ?,?,? WHERE (SELECT privacy_mode FROM settings WHERE id=1)=0
	ON CONFLICT(device_id,started) DO UPDATE SET note=excluded.note`, id, start.UTC().Format(tripNoteTime), note)
	return err
}
