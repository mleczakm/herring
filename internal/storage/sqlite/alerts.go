package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

const alertsSchema = `
CREATE TABLE IF NOT EXISTS saved_places (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    radius_m REAL NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS alert_settings (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id INTEGER NOT NULL REFERENCES managed_devices(id) ON DELETE CASCADE,
    entry INTEGER NOT NULL,
    exit INTEGER NOT NULL,
    speed_kph REAL NOT NULL,
    humor INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(user_id,device_id)
) STRICT;

CREATE TABLE IF NOT EXISTS alert_place_state (
    user_id INTEGER NOT NULL,
    device_id INTEGER NOT NULL,
    place_id INTEGER NOT NULL REFERENCES saved_places(id) ON DELETE CASCADE,
    inside INTEGER NOT NULL,
    tracker_time TEXT NOT NULL,
    PRIMARY KEY(user_id,device_id,place_id),
    FOREIGN KEY(user_id,device_id) REFERENCES alert_settings(user_id,device_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE IF NOT EXISTS alert_selected_places (
    user_id INTEGER NOT NULL,
    device_id INTEGER NOT NULL,
    place_id INTEGER NOT NULL REFERENCES saved_places(id) ON DELETE CASCADE,
    PRIMARY KEY(user_id,device_id,place_id),
    FOREIGN KEY(user_id,device_id) REFERENCES alert_settings(user_id,device_id) ON DELETE CASCADE
) STRICT;

CREATE TABLE IF NOT EXISTS alert_speed_state (
    user_id INTEGER NOT NULL,
    device_id INTEGER NOT NULL,
    over_limit INTEGER NOT NULL,
    tracker_time TEXT NOT NULL,
    PRIMARY KEY(user_id,device_id),
    FOREIGN KEY(user_id,device_id) REFERENCES alert_settings(user_id,device_id) ON DELETE CASCADE
) STRICT;

CREATE TRIGGER IF NOT EXISTS alerts_privacy_reset
AFTER UPDATE OF privacy_mode, privacy_client_hash ON settings
WHEN OLD.privacy_mode != NEW.privacy_mode OR OLD.privacy_client_hash != NEW.privacy_client_hash
BEGIN
    DELETE FROM alert_place_state;
    DELETE FROM alert_speed_state;
END;

CREATE TRIGGER IF NOT EXISTS alerts_forget_expired_fix
AFTER DELETE ON positions
BEGIN
    DELETE FROM alert_place_state WHERE device_id IN
        (SELECT id FROM managed_devices WHERE tracker_id = OLD.device_id) AND tracker_time = OLD.tracker_time;
    DELETE FROM alert_speed_state WHERE device_id IN
        (SELECT id FROM managed_devices WHERE tracker_id = OLD.device_id) AND tracker_time = OLD.tracker_time;
END;
`

type Place struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	RadiusM   float64 `json:"radius_m"`
}
type AlertSettings struct {
	Entry    bool    `json:"entry"`
	Exit     bool    `json:"exit"`
	SpeedKPH float64 `json:"speed_kph"`
	Humor    bool    `json:"humor"`
	PlaceIDs []int64 `json:"place_ids"`
}
type PlaceSuggestion struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Visits    int     `json:"visits"`
}
type AlertEvent struct {
	UserID     int64
	DeviceName string
	Kind       string
	PlaceName  string
	SpeedKPH   float64
	Humor      bool
}

func (s *Store) Places(ctx context.Context, userID int64) ([]Place, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT id,name,latitude,longitude,radius_m FROM saved_places WHERE user_id=? ORDER BY name,id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Place{}
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.ID, &p.Name, &p.Latitude, &p.Longitude, &p.RadiusM); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Store) CreatePlace(ctx context.Context, userID int64, p Place) (Place, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 100 || !validCoordinates(p.Latitude, p.Longitude) || math.IsNaN(p.RadiusM) || p.RadiusM < 25 || p.RadiusM > 10000 {
		return Place{}, errors.New("invalid place")
	}
	res, err := s.database.ExecContext(ctx, `INSERT INTO saved_places(user_id,name,latitude,longitude,radius_m) VALUES(?,?,?,?,?)`, userID, p.Name, p.Latitude, p.Longitude, p.RadiusM)
	if err != nil {
		return Place{}, err
	}
	p.ID, err = res.LastInsertId()
	return p, err
}
func validCoordinates(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func (s *Store) DeletePlace(ctx context.Context, userID, id int64) (bool, error) {
	r, e := s.database.ExecContext(ctx, `DELETE FROM saved_places WHERE id=? AND user_id=?`, id, userID)
	if e != nil {
		return false, e
	}
	n, e := r.RowsAffected()
	return n > 0, e
}
func (s *Store) DeviceAlerts(ctx context.Context, userID, deviceID int64) (AlertSettings, error) {
	a := AlertSettings{PlaceIDs: []int64{}}
	var exists bool
	if err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM managed_devices WHERE id=?)`, deviceID).Scan(&exists); err != nil {
		return a, err
	}
	if !exists {
		return a, sql.ErrNoRows
	}
	err := s.database.QueryRowContext(ctx, `SELECT entry,exit,speed_kph,humor FROM alert_settings WHERE user_id=? AND device_id=?`, userID, deviceID).Scan(&a.Entry, &a.Exit, &a.SpeedKPH, &a.Humor)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	rows, err := s.database.QueryContext(ctx, `SELECT place_id FROM alert_selected_places WHERE user_id=? AND device_id=? ORDER BY place_id`, userID, deviceID)
	if err != nil {
		return a, err
	}
	defer rows.Close()
	a.PlaceIDs = []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return a, err
		}
		a.PlaceIDs = append(a.PlaceIDs, id)
	}
	return a, rows.Err()
}
func (s *Store) SaveDeviceAlerts(ctx context.Context, userID, deviceID int64, a AlertSettings) error {
	if math.IsNaN(a.SpeedKPH) || a.SpeedKPH < 0 || a.SpeedKPH > 500 {
		return errors.New("invalid speed")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `INSERT INTO alert_settings(user_id,device_id,entry,exit,speed_kph,humor) SELECT ?,id,?,?,?,? FROM managed_devices WHERE id=? ON CONFLICT(user_id,device_id) DO UPDATE SET entry=excluded.entry,exit=excluded.exit,speed_kph=excluded.speed_kph,humor=excluded.humor`, userID, a.Entry, a.Exit, a.SpeedKPH, a.Humor, deviceID)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM alert_selected_places WHERE user_id=? AND device_id=?`, userID, deviceID); err != nil {
		return err
	}
	for _, id := range a.PlaceIDs {
		r, err := tx.ExecContext(ctx, `INSERT INTO alert_selected_places(user_id,device_id,place_id) SELECT ?,?,id FROM saved_places WHERE id=? AND user_id=?`, userID, deviceID, id, userID)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return errors.New("invalid place selection")
		}
	}
	// Re-baseline after changing settings to avoid fabricating transitions.
	if _, err = tx.ExecContext(ctx, `DELETE FROM alert_place_state WHERE user_id=? AND device_id=?`, userID, deviceID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM alert_speed_state WHERE user_id=? AND device_id=?`, userID, deviceID); err != nil {
		return err
	}
	return tx.Commit()
}

// Suggestions are calculated from retained stationary fixes only; no extra history is stored.
func (s *Store) PlaceSuggestions(ctx context.Context, userID int64, now time.Time) ([]PlaceSuggestion, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if settings.PrivacyMode {
		return []PlaceSuggestion{}, nil
	}
	cutoff := ""
	if settings.ServerRetentionDays > 0 {
		cutoff = formatTime(now.AddDate(0, 0, -settings.ServerRetentionDays))
	}
	rows, err := s.database.QueryContext(ctx, `
        WITH fixes AS (
            SELECT device_id, unixepoch(tracker_time) AS stamp, substr(tracker_time,1,10) AS day,
                   round(latitude,3) AS lat, round(longitude,3) AS lon, speed_kph < 3 AS still
            FROM positions
            WHERE gps_valid = 1 AND device_id IN (SELECT tracker_id FROM managed_devices)
              AND (SELECT privacy_mode FROM settings WHERE id=1) = 0 AND (?='' OR received_at >= ?)
        ), boundaries AS (
            SELECT *, CASE WHEN still AND lag(still) OVER device
                                    AND lat = lag(lat) OVER device AND lon = lag(lon) OVER device
                                    AND stamp - lag(stamp) OVER device <= 1800 THEN 0 ELSE 1 END AS new_stop
            FROM fixes WINDOW device AS (PARTITION BY device_id ORDER BY stamp)
        ), runs AS (
            SELECT *, sum(new_stop) OVER (PARTITION BY device_id ORDER BY stamp) AS stop FROM boundaries
        ), stops AS (
            SELECT device_id, lat, lon, day, stop FROM runs WHERE still
            GROUP BY device_id, lat, lon, day, stop HAVING max(stamp) - min(stamp) >= 300
        )
        SELECT lat, lon, count(DISTINCT day) AS days FROM stops
        GROUP BY lat, lon HAVING days >= 2 ORDER BY days DESC, lat, lon LIMIT 20`, cutoff, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlaceSuggestion{}
	for rows.Next() {
		var p PlaceSuggestion
		if err = rows.Scan(&p.Latitude, &p.Longitude, &p.Visits); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	places, err := s.Places(ctx, userID)
	if err != nil {
		return nil, err
	}
	filtered := []PlaceSuggestion{}
	for _, p := range out {
		near := false
		for _, saved := range places {
			if distanceM(p.Latitude, p.Longitude, saved.Latitude, saved.Longitude) <= math.Max(saved.RadiusM, 150) {
				near = true
				break
			}
		}
		if !near {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}
func distanceM(a, b, c, d float64) float64 {
	const rad = math.Pi / 180
	x := math.Sin((c - a) * rad / 2)
	y := math.Sin((d - b) * rad / 2)
	v := x*x + math.Cos(a*rad)*math.Cos(c*rad)*y*y
	return 12742000 * math.Asin(math.Sqrt(math.Min(1, v)))
}

// EvaluateAlerts baselines the first valid fix and emits only subsequent transitions.
// It stores state and timestamps, never the device's coordinates.
func (s *Store) EvaluateAlerts(ctx context.Context, loc sinotrack.Location) ([]AlertEvent, error) {
	if !loc.GPSValid || !validCoordinates(loc.Latitude, loc.Longitude) {
		return nil, nil
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var privacy bool
	if err = tx.QueryRowContext(ctx, `SELECT privacy_mode FROM settings WHERE id=1`).Scan(&privacy); err != nil {
		return nil, err
	}
	if privacy {
		return nil, nil
	}
	type config struct {
		user, device int64
		name         string
		entry, exit  bool
		speed        float64
		humor        bool
	}
	var configs []config
	rows, err := tx.QueryContext(ctx, `SELECT a.user_id,a.device_id,m.name,a.entry,a.exit,a.speed_kph,a.humor FROM alert_settings a JOIN managed_devices m ON m.id=a.device_id WHERE m.tracker_id=?`, loc.DeviceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c config
		if err = rows.Scan(&c.user, &c.device, &c.name, &c.entry, &c.exit, &c.speed, &c.humor); err != nil {
			rows.Close()
			return nil, err
		}
		configs = append(configs, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	stamp := loc.TrackerTime.UTC().Format("2006-01-02T15:04:05.000000000Z")
	events := []AlertEvent{}
	for _, c := range configs {
		rows, err = tx.QueryContext(ctx, `SELECT id,name,latitude,longitude,radius_m FROM saved_places WHERE user_id=? AND id IN (SELECT place_id FROM alert_selected_places WHERE user_id=? AND device_id=?)`, c.user, c.user, c.device)
		if err != nil {
			return nil, err
		}
		var places []Place
		for rows.Next() {
			var p Place
			if err = rows.Scan(&p.ID, &p.Name, &p.Latitude, &p.Longitude, &p.RadiusM); err != nil {
				rows.Close()
				return nil, err
			}
			places = append(places, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, p := range places {
			if !c.entry && !c.exit {
				break
			}
			inside := distanceM(loc.Latitude, loc.Longitude, p.Latitude, p.Longitude) <= p.RadiusM
			var previous bool
			var last string
			e := tx.QueryRowContext(ctx, `SELECT inside,tracker_time FROM alert_place_state WHERE user_id=? AND device_id=? AND place_id=?`, c.user, c.device, p.ID).Scan(&previous, &last)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil && stamp <= last {
				continue
			}
			if e == nil && inside != previous && ((inside && c.entry) || (!inside && c.exit)) {
				kind := "exit"
				if inside {
					kind = "entry"
				}
				events = append(events, AlertEvent{UserID: c.user, DeviceName: c.name, Kind: kind, PlaceName: p.Name, Humor: c.humor})
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO alert_place_state(user_id,device_id,place_id,inside,tracker_time) VALUES(?,?,?,?,?) ON CONFLICT(user_id,device_id,place_id) DO UPDATE SET inside=excluded.inside,tracker_time=excluded.tracker_time`, c.user, c.device, p.ID, inside, stamp); err != nil {
				return nil, err
			}
		}
		if c.speed == 0 {
			continue
		}
		var previous bool
		var last string
		e := tx.QueryRowContext(ctx, `SELECT over_limit,tracker_time FROM alert_speed_state WHERE user_id=? AND device_id=?`, c.user, c.device).Scan(&previous, &last)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if e == nil && stamp <= last {
			continue
		}
		over := c.speed > 0 && loc.SpeedKPH > c.speed
		if e == nil && over && !previous {
			events = append(events, AlertEvent{UserID: c.user, DeviceName: c.name, Kind: "speed", SpeedKPH: loc.SpeedKPH, Humor: c.humor})
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO alert_speed_state(user_id,device_id,over_limit,tracker_time) VALUES(?,?,?,?) ON CONFLICT(user_id,device_id) DO UPDATE SET over_limit=excluded.over_limit,tracker_time=excluded.tracker_time`, c.user, c.device, over, stamp); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}
