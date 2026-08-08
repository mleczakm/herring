// Package sqlite provides Herring's durable SQLite storage.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL COLLATE NOCASE UNIQUE,
    display_name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin')),
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS managed_devices (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL CHECK (model IN ('st901-2g', 'st901-4g')),
    phone_number TEXT NOT NULL UNIQUE,
    tracker_id TEXT UNIQUE,
    config_status TEXT NOT NULL CHECK (config_status IN ('sending','awaiting_reply','configured','failed')),
    config_detail TEXT NOT NULL DEFAULT '',
    config_started_at TEXT NOT NULL,
    config_verified_at TEXT
) STRICT;

CREATE TABLE IF NOT EXISTS sms_commands (
    id INTEGER PRIMARY KEY,
    managed_device_id INTEGER NOT NULL REFERENCES managed_devices(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    provider_message_id TEXT UNIQUE,
    delivery_status TEXT NOT NULL,
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

type User struct {
	ID                               int64
	Email, DisplayName, PasswordHash string
}
type ManagedDevice struct {
	ID                                                              int64
	Name, Model, PhoneNumber, TrackerID, ConfigStatus, ConfigDetail string
	ConfigStartedAt                                                 time.Time
	ConfigVerifiedAt                                                *time.Time
}

// ErrSetupComplete means the initial administrator already exists.
var ErrSetupComplete = errors.New("initial setup is already complete")

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

// SetupRequired reports whether the database has no users yet.
func (s *Store) SetupRequired(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users)").Scan(&exists); err != nil {
		return false, fmt.Errorf("check initial setup: %w", err)
	}
	return !exists, nil
}

// CreateInitialAdmin atomically creates the first and only bootstrap user.
// Password hashing is intentionally performed before entering the storage
// boundary so the transaction remains short.
func (s *Store) CreateInitialAdmin(ctx context.Context, email, displayName, passwordHash string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	displayName = strings.TrimSpace(displayName)
	if email == "" || displayName == "" || passwordHash == "" {
		return fmt.Errorf("initial administrator fields must not be empty")
	}

	result, err := s.database.ExecContext(ctx, `
        INSERT INTO users (email, display_name, password_hash, role, created_at)
        SELECT ?, ?, ?, 'admin', ?
        WHERE NOT EXISTS (SELECT 1 FROM users)`,
		email, displayName, passwordHash, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("create initial administrator: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check initial administrator result: %w", err)
	}
	if rows == 0 {
		return ErrSetupComplete
	}
	return nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.database.QueryRowContext(ctx, `SELECT id,email,display_name,password_hash FROM users WHERE email = ?`, strings.TrimSpace(strings.ToLower(email))).Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`, tokenHash, userID, formatTime(expiresAt), formatTime(time.Now()))
	return err
}

func (s *Store) SessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	var user User
	err := s.database.QueryRowContext(ctx, `SELECT u.id,u.email,u.display_name,u.password_hash FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash, formatTime(now)).Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash)
	return user, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *Store) CreateManagedDevice(ctx context.Context, name, model, phone string) (ManagedDevice, error) {
	now := time.Now()
	result, err := s.database.ExecContext(ctx, `INSERT INTO managed_devices(name,model,phone_number,config_status,config_started_at) VALUES(?,?,?,'sending',?)`, strings.TrimSpace(name), model, phone, formatTime(now))
	if err != nil {
		return ManagedDevice{}, fmt.Errorf("create managed device: %w", err)
	}
	id, _ := result.LastInsertId()
	return ManagedDevice{ID: id, Name: strings.TrimSpace(name), Model: model, PhoneNumber: phone, ConfigStatus: "sending", ConfigStartedAt: now}, nil
}

func (s *Store) ManagedDevices(ctx context.Context) ([]ManagedDevice, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT id,name,model,phone_number,COALESCE(tracker_id,''),config_status,config_detail,config_started_at,config_verified_at FROM managed_devices ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ManagedDevice
	for rows.Next() {
		d, err := scanManaged(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ManagedDeviceByID(ctx context.Context, id int64) (ManagedDevice, error) {
	return scanManaged(s.database.QueryRowContext(ctx, `SELECT id,name,model,phone_number,COALESCE(tracker_id,''),config_status,config_detail,config_started_at,config_verified_at FROM managed_devices WHERE id=?`, id))
}
func (s *Store) ManagedDeviceByPhone(ctx context.Context, phone string) (ManagedDevice, error) {
	return scanManaged(s.database.QueryRowContext(ctx, `SELECT id,name,model,phone_number,COALESCE(tracker_id,''),config_status,config_detail,config_started_at,config_verified_at FROM managed_devices WHERE phone_number=?`, phone))
}

type scanner interface{ Scan(...any) error }

func scanManaged(row scanner) (ManagedDevice, error) {
	var d ManagedDevice
	var started string
	var verified sql.NullString
	err := row.Scan(&d.ID, &d.Name, &d.Model, &d.PhoneNumber, &d.TrackerID, &d.ConfigStatus, &d.ConfigDetail, &started, &verified)
	if err != nil {
		return d, err
	}
	d.ConfigStartedAt, _ = time.Parse(time.RFC3339Nano, started)
	if verified.Valid {
		t, _ := time.Parse(time.RFC3339Nano, verified.String)
		d.ConfigVerifiedAt = &t
	}
	return d, nil
}

func (s *Store) RecordSMSCommand(ctx context.Context, deviceID int64, kind, messageID, status string) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO sms_commands(managed_device_id,kind,provider_message_id,delivery_status,created_at) VALUES(?,?,?,?,?)`, deviceID, kind, messageID, status, formatTime(time.Now()))
	return err
}
func (s *Store) UpdateSMSDelivery(ctx context.Context, messageID, status string) error {
	_, err := s.database.ExecContext(ctx, `UPDATE sms_commands SET delivery_status=? WHERE provider_message_id=?`, status, messageID)
	return err
}
func (s *Store) SetConfigurationStatus(ctx context.Context, id int64, status, detail string) error {
	var verified any
	if status == "configured" {
		verified = formatTime(time.Now())
	}
	_, err := s.database.ExecContext(ctx, `UPDATE managed_devices SET config_status=?,config_detail=?,config_verified_at=COALESCE(?,config_verified_at) WHERE id=?`, status, detail, verified, id)
	return err
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
