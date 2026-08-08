// Package sqlite provides Herring's durable SQLite storage.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS positions (
    id INTEGER PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    tracker_time TEXT NOT NULL,
    received_at TEXT NOT NULL,
    gps_valid INTEGER NOT NULL CHECK (gps_valid IN (0, 1)),
    latitude REAL NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude REAL NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    speed_kph REAL NOT NULL CHECK (speed_kph >= 0),
    heading REAL NOT NULL CHECK (heading >= 0 AND heading < 360),
    status TEXT NOT NULL,
    mcc INTEGER NOT NULL,
    mnc INTEGER NOT NULL,
    raw_frame TEXT NOT NULL,
    UNIQUE (device_id, tracker_time, raw_frame)
) STRICT;

CREATE INDEX IF NOT EXISTS positions_device_time_idx
    ON positions (device_id, tracker_time DESC);
`

// Store owns a deliberately small connection pool. SQLite WAL supports
// concurrent readers, while one application write connection avoids lock
// contention on a small VPS.
type Store struct {
	database *sql.DB
}

// Open opens the database, configures connection safety pragmas, and applies
// idempotent schema migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("SQLite path must not be empty")
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	store := &Store{database: database}
	if err := store.initialize(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.database.Close()
}

// RegisterDevice makes a tracker identifier eligible for ingestion.
func (s *Store) RegisterDevice(ctx context.Context, deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device ID must not be empty")
	}
	_, err := s.database.ExecContext(ctx, `
        INSERT INTO devices (id, created_at) VALUES (?, ?)
        ON CONFLICT (id) DO NOTHING`, deviceID, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

// SaveLocation persists a validated position. The foreign key intentionally
// rejects identifiers that have not been registered.
func (s *Store) SaveLocation(ctx context.Context, receivedAt time.Time, location sinotrack.Location) error {
	_, err := s.database.ExecContext(ctx, `
        INSERT INTO positions (
            device_id, tracker_time, received_at, gps_valid, latitude, longitude,
            speed_kph, heading, status, mcc, mnc, raw_frame
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT (device_id, tracker_time, raw_frame) DO NOTHING`,
		location.DeviceID,
		formatTime(location.TrackerTime),
		formatTime(receivedAt),
		location.GPSValid,
		location.Latitude,
		location.Longitude,
		location.SpeedKPH,
		location.Heading,
		location.Status,
		location.MCC,
		location.MNC,
		location.Raw,
	)
	if err != nil {
		return fmt.Errorf("save location for device %s: %w", location.DeviceID, err)
	}
	return nil
}

func (s *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		schema,
	} {
		if _, err := s.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize SQLite: %w", err)
		}
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
