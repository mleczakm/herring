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

CREATE INDEX IF NOT EXISTS positions_received_idx
    ON positions (received_at);

-- Installation-wide settings. Herring has a single administrator, so one row
-- holds both the privacy binding and the retention policy.
CREATE TABLE IF NOT EXISTS settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    privacy_mode INTEGER NOT NULL DEFAULT 0 CHECK (privacy_mode IN (0, 1)),
    privacy_client_hash TEXT NOT NULL DEFAULT '',
    privacy_client_label TEXT NOT NULL DEFAULT '',
    privacy_bound_at TEXT NOT NULL DEFAULT '',
    server_retention_days INTEGER NOT NULL DEFAULT 0 CHECK (server_retention_days BETWEEN 0 AND 3650),
    browser_retention_days INTEGER NOT NULL DEFAULT 0 CHECK (browser_retention_days BETWEEN 0 AND 3650)
) STRICT;

INSERT OR IGNORE INTO settings (id) VALUES (1);
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

// LinkTrackerIfUnambiguous associates an incoming tracker device ID with a
// managed device the first time it reports in. It only links automatically
// when exactly one managed device is still awaiting a tracker link, since a
// phone-number-only setup has no other way to tell trackers apart.
//
// Only identifiers registered through HERRING_DEVICE_IDS are linked. The TCP
// port is public, so an unknown sender must never claim a pending device:
// its positions are rejected anyway and the link would hide the real tracker.
func (s *Store) LinkTrackerIfUnambiguous(ctx context.Context, deviceID string) error {
	_, err := s.database.ExecContext(ctx, `
        UPDATE managed_devices SET tracker_id=?
        WHERE tracker_id IS NULL
          AND EXISTS (SELECT 1 FROM devices WHERE id=?)
          AND NOT EXISTS (SELECT 1 FROM managed_devices WHERE tracker_id=?)
          AND (SELECT COUNT(*) FROM managed_devices WHERE tracker_id IS NULL) = 1`,
		deviceID, deviceID, deviceID)
	if err != nil {
		return fmt.Errorf("link tracker %s: %w", deviceID, err)
	}
	return nil
}

// DevicePosition is a managed device paired with its latest known position,
// if any tracker has linked and reported in yet.
type DevicePosition struct {
	Device                                 ManagedDevice
	HasPosition                            bool
	Latitude, Longitude, SpeedKPH, Heading float64
	GPSValid                               bool
	TrackerTime, ReceivedAt                time.Time
	// Source is "gprs" for tracker frames and "sms" for SMS replies.
	Source string
}

// LatestPositions returns every managed device paired with its most recent
// position, ordered like ManagedDevices. Devices with no linked tracker or no
// reports yet come back with HasPosition false.
func (s *Store) LatestPositions(ctx context.Context) ([]DevicePosition, error) {
	rows, err := s.database.QueryContext(ctx, `
        SELECT md.id, md.name, md.model, md.phone_number, COALESCE(md.tracker_id,''), md.config_status, md.config_detail, md.config_started_at, md.config_verified_at,
               p.latitude, p.longitude, p.speed_kph, p.heading, p.gps_valid, p.tracker_time, p.received_at, p.source
        FROM managed_devices md
        LEFT JOIN positions p ON p.id = (
            SELECT p2.id FROM positions p2 WHERE p2.device_id = md.tracker_id ORDER BY p2.tracker_time DESC LIMIT 1
        )
        ORDER BY md.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("query latest positions: %w", err)
	}
	defer rows.Close()
	var out []DevicePosition
	for rows.Next() {
		var (
			dp                    DevicePosition
			started               string
			verified              sql.NullString
			lat, lng, speed, head sql.NullFloat64
			gpsValid              sql.NullBool
			trackerTime, received sql.NullString
			source                sql.NullString
		)
		if err := rows.Scan(&dp.Device.ID, &dp.Device.Name, &dp.Device.Model, &dp.Device.PhoneNumber, &dp.Device.TrackerID, &dp.Device.ConfigStatus, &dp.Device.ConfigDetail, &started, &verified,
			&lat, &lng, &speed, &head, &gpsValid, &trackerTime, &received, &source); err != nil {
			return nil, fmt.Errorf("scan latest position: %w", err)
		}
		dp.Device.ConfigStartedAt, _ = time.Parse(time.RFC3339Nano, started)
		if verified.Valid {
			t, _ := time.Parse(time.RFC3339Nano, verified.String)
			dp.Device.ConfigVerifiedAt = &t
		}
		if lat.Valid {
			dp.HasPosition = true
			dp.Latitude, dp.Longitude, dp.SpeedKPH, dp.Heading = lat.Float64, lng.Float64, speed.Float64, head.Float64
			dp.GPSValid = gpsValid.Bool
			dp.TrackerTime, _ = time.Parse(time.RFC3339Nano, trackerTime.String)
			dp.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received.String)
			dp.Source = source.String
		}
		out = append(out, dp)
	}
	return out, rows.Err()
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
		alertsSchema,
	} {
		if _, err := s.database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize SQLite: %w", err)
		}
	}
	if err := s.InitPush(ctx); err != nil {
		return err
	}
	return s.InitContact(ctx)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// MaxRetentionDays bounds both retention settings (about ten years).
const MaxRetentionDays = 3650

// Settings is the installation-wide privacy and retention configuration.
// A retention of 0 days keeps data indefinitely.
type Settings struct {
	PrivacyMode          bool
	PrivacyClientHash    string
	PrivacyClientLabel   string
	PrivacyBoundAt       time.Time
	ServerRetentionDays  int
	BrowserRetentionDays int
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var (
		settings Settings
		boundAt  string
	)
	err := s.database.QueryRowContext(ctx, `
        SELECT privacy_mode, privacy_client_hash, privacy_client_label, privacy_bound_at,
               server_retention_days, browser_retention_days
        FROM settings WHERE id = 1`).Scan(&settings.PrivacyMode, &settings.PrivacyClientHash, &settings.PrivacyClientLabel,
		&boundAt, &settings.ServerRetentionDays, &settings.BrowserRetentionDays)
	if err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	if boundAt != "" {
		settings.PrivacyBoundAt, _ = time.Parse(time.RFC3339Nano, boundAt)
	}
	return settings, nil
}

// SaveRetention stores how many days positions are kept on the server and in
// the privacy-mode browser.
func (s *Store) SaveRetention(ctx context.Context, serverDays, browserDays int) error {
	if serverDays < 0 || serverDays > MaxRetentionDays || browserDays < 0 || browserDays > MaxRetentionDays {
		return fmt.Errorf("retention must be between 0 and %d days", MaxRetentionDays)
	}
	_, err := s.database.ExecContext(ctx, `UPDATE settings SET server_retention_days = ?, browser_retention_days = ? WHERE id = 1`, serverDays, browserDays)
	if err != nil {
		return fmt.Errorf("save retention: %w", err)
	}
	return nil
}

// EnablePrivacy binds privacy mode to one browser, identified by the hash of a
// secret only that browser holds. Binding a new browser replaces the old one.
func (s *Store) EnablePrivacy(ctx context.Context, clientHash, label string, now time.Time) error {
	if clientHash == "" {
		return fmt.Errorf("privacy client hash must not be empty")
	}
	_, err := s.database.ExecContext(ctx, `
        UPDATE settings SET privacy_mode = 1, privacy_client_hash = ?, privacy_client_label = ?, privacy_bound_at = ?
        WHERE id = 1`, clientHash, label, formatTime(now))
	if err != nil {
		return fmt.Errorf("enable privacy mode: %w", err)
	}
	return nil
}

// DisablePrivacy turns privacy mode off. Positions already transferred to the
// bound browser stay there; the server starts keeping new positions again.
func (s *Store) DisablePrivacy(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, `
        UPDATE settings SET privacy_mode = 0, privacy_client_hash = '', privacy_client_label = '', privacy_bound_at = ''
        WHERE id = 1`)
	if err != nil {
		return fmt.Errorf("disable privacy mode: %w", err)
	}
	return nil
}

// PurgePositionsReceivedBefore deletes positions the server received before
// cutoff and reports how many were removed.
func (s *Store) PurgePositionsReceivedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.database.ExecContext(ctx, `DELETE FROM positions WHERE received_at < ?`, formatTime(cutoff))
	if err != nil {
		return 0, fmt.Errorf("purge positions: %w", err)
	}
	return result.RowsAffected()
}

// StoredPosition is a position row as handed to the privacy-mode browser.
type StoredPosition struct {
	ID                                     int64
	DeviceID                               string
	TrackerTime, ReceivedAt                time.Time
	GPSValid                               bool
	Latitude, Longitude, SpeedKPH, Heading float64
	Source                                 string
}

// PositionKey identifies a stored position precisely. SQLite may reuse a
// rowid after the newest rows are deleted, so the ID alone is not enough to
// acknowledge a transfer safely.
type PositionKey struct {
	ID          int64
	DeviceID    string
	TrackerTime time.Time
}

// OldestPositions returns up to limit stored positions, oldest first.
func (s *Store) OldestPositions(ctx context.Context, limit int) ([]StoredPosition, error) {
	rows, err := s.database.QueryContext(ctx, `
        SELECT id, device_id, tracker_time, received_at, gps_valid, latitude, longitude, speed_kph, heading, source
        FROM positions ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query stored positions: %w", err)
	}
	defer rows.Close()
	out := []StoredPosition{}
	for rows.Next() {
		var (
			p                     StoredPosition
			trackerTime, received string
		)
		if err := rows.Scan(&p.ID, &p.DeviceID, &trackerTime, &received, &p.GPSValid, &p.Latitude, &p.Longitude, &p.SpeedKPH, &p.Heading, &p.Source); err != nil {
			return nil, fmt.Errorf("scan stored position: %w", err)
		}
		p.TrackerTime, _ = time.Parse(time.RFC3339Nano, trackerTime)
		p.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePositions removes exactly the given positions in one transaction and
// reports how many matched.
func (s *Store) DeletePositions(ctx context.Context, keys []PositionKey) (int64, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin position delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	statement, err := tx.PrepareContext(ctx, `DELETE FROM positions WHERE id = ? AND device_id = ? AND tracker_time = ?`)
	if err != nil {
		return 0, fmt.Errorf("prepare position delete: %w", err)
	}
	defer statement.Close()
	var deleted int64
	for _, key := range keys {
		result, err := statement.ExecContext(ctx, key.ID, key.DeviceID, formatTime(key.TrackerTime))
		if err != nil {
			return 0, fmt.Errorf("delete position %d: %w", key.ID, err)
		}
		n, _ := result.RowsAffected()
		deleted += n
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit position delete: %w", err)
	}
	return deleted, nil
}

// ApplyServerRetention deletes positions older than the configured server
// retention. It does nothing when retention is unlimited (0 days).
func (s *Store) ApplyServerRetention(ctx context.Context, now time.Time) (int64, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return 0, err
	}
	if settings.ServerRetentionDays == 0 {
		return 0, nil
	}
	return s.PurgePositionsReceivedBefore(ctx, now.AddDate(0, 0, -settings.ServerRetentionDays))
}

// CountPositions reports how many positions the server currently stores.
func (s *Store) CountPositions(ctx context.Context) (int64, error) {
	var count int64
	if err := s.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM positions`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count positions: %w", err)
	}
	return count, nil
}
