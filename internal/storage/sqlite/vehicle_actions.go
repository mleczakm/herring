package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

var Warsaw, _ = time.LoadLocation("Europe/Warsaw")

type Reminder struct {
	ID          int64
	Title, Date string
	Meters      float64
	Notified    bool
}

func (s *Store) Reminders(ctx context.Context, id int64) ([]Reminder, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT id,title,due_date,due_m,notified FROM service_reminders WHERE device_id=? ORDER BY notified,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reminder
	for rows.Next() {
		var r Reminder
		if err := rows.Scan(&r.ID, &r.Title, &r.Date, &r.Meters, &r.Notified); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) AddReminder(ctx context.Context, id, user int64, title, date string, meters float64) error {
	_, err := s.database.ExecContext(ctx, `INSERT INTO service_reminders(device_id,user_id,title,due_date,due_m) VALUES(?,?,?,?,?)`, id, user, title, date, meters)
	return err
}
func (s *Store) DeleteReminder(ctx context.Context, id, reminder int64) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM service_reminders WHERE device_id=? AND id=?`, id, reminder)
	return err
}

type ScheduledVehicleEvent struct {
	AlertEvent
	ReminderID int64
	Month      string
}

func (s *Store) DueReminders(ctx context.Context, now time.Time) ([]ScheduledVehicleEvent, error) {
	rows, err := s.database.QueryContext(ctx, `SELECT r.id,r.user_id,r.device_id,d.name,r.title
	FROM service_reminders r JOIN managed_devices d ON d.id=r.device_id
	WHERE r.notified=0 AND EXISTS (SELECT 1 FROM push_subscriptions WHERE user_id=r.user_id)
	AND ((r.due_date<>'' AND r.due_date<=?) OR (r.due_m>0 AND r.due_m<=
	(SELECT odometer_m FROM vehicle_profiles WHERE device_id=r.device_id)))
	ORDER BY r.id`, now.In(Warsaw).Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScheduledVehicleEvent
	for rows.Next() {
		event := ScheduledVehicleEvent{AlertEvent: AlertEvent{Kind: "service"}}
		if err := rows.Scan(&event.ReminderID, &event.UserID, &event.DeviceID, &event.DeviceName, &event.PlaceName); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) MarkVehicleEventSent(ctx context.Context, event ScheduledVehicleEvent) error {
	if event.ReminderID != 0 {
		_, err := s.database.ExecContext(ctx, `UPDATE service_reminders SET notified=1 WHERE id=? AND device_id=? AND user_id=? AND title=?`, event.ReminderID, event.DeviceID, event.UserID, event.PlaceName)
		return err
	}
	_, err := s.database.ExecContext(ctx, `UPDATE monthly_reports SET last_month=? WHERE user_id=? AND last_month<?`, event.Month, event.UserID, event.Month)
	return err
}

type Guard struct {
	Radius    float64
	Triggered bool
}

func (s *Store) Guard(ctx context.Context, id int64) (Guard, error) {
	var g Guard
	err := s.database.QueryRowContext(ctx, `SELECT radius,triggered FROM vehicle_guards WHERE device_id=?`, id).Scan(&g.Radius, &g.Triggered)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return g, err
}
func (s *Store) SetGuard(ctx context.Context, id, user int64, radius float64, now time.Time) (bool, error) {
	if radius == 0 {
		_, err := s.database.ExecContext(ctx, `DELETE FROM vehicle_guards WHERE device_id=?`, id)
		return true, err
	}
	result, err := s.database.ExecContext(ctx, `INSERT INTO vehicle_guards(device_id,user_id,lat,lon,radius,last_fix,outside,triggered)
	SELECT d.id,?,p.latitude,p.longitude,?,p.tracker_time,0,0 FROM managed_devices d JOIN positions p ON p.device_id=d.tracker_id
	WHERE d.id=? AND p.gps_valid=1 AND p.source='gprs' AND p.speed_kph<3
	AND p.tracker_time>=? AND p.tracker_time<=?
	AND (SELECT privacy_mode FROM settings WHERE id=1)=0
	AND p.id=(SELECT id FROM positions WHERE device_id=d.tracker_id AND source='gprs' ORDER BY julianday(tracker_time) DESC,id DESC LIMIT 1)
	ON CONFLICT(device_id) DO UPDATE SET user_id=excluded.user_id,lat=excluded.lat,lon=excluded.lon,radius=excluded.radius,last_fix=excluded.last_fix,outside=0,triggered=0`,
		user, radius, id, formatTime(now.Add(-10*time.Minute)), formatTime(now.Add(time.Minute)))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n != 0, err
}
func (s *Store) EvaluateGuard(ctx context.Context, loc sinotrack.Location, now time.Time) (*AlertEvent, error) {
	if !loc.GPSValid || loc.TrackerTime.Before(now.Add(-10*time.Minute)) || loc.TrackerTime.After(now.Add(time.Minute)) {
		return nil, nil
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	event := AlertEvent{Kind: "guard"}
	var lat, lon, radius float64
	var last string
	var outside int
	err = tx.QueryRowContext(ctx, `SELECT g.device_id,g.user_id,d.name,g.lat,g.lon,g.radius,g.last_fix,g.outside
	FROM vehicle_guards g JOIN managed_devices d ON d.id=g.device_id
	WHERE d.tracker_id=? AND g.triggered=0 AND (SELECT privacy_mode FROM settings WHERE id=1)=0`, loc.DeviceID).
		Scan(&event.DeviceID, &event.UserID, &event.DeviceName, &lat, &lon, &radius, &last, &outside)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	previous, _ := time.Parse(time.RFC3339Nano, last)
	if !loc.TrackerTime.After(previous) {
		return nil, nil
	}
	if loc.TrackerTime.Sub(previous) > 10*time.Minute {
		outside = 0
	}
	distance := DistanceM(lat, lon, loc.Latitude, loc.Longitude)
	if distance > radius || (distance >= 15 && loc.SpeedKPH >= 5) {
		outside++
	} else {
		outside = 0
	}
	triggered := outside >= 2
	_, err = tx.ExecContext(ctx, `UPDATE vehicle_guards SET outside=?,last_fix=?,triggered=? WHERE device_id=?`, outside, formatTime(loc.TrackerTime), triggered, event.DeviceID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil || !triggered {
		return nil, err
	}
	return &event, nil
}

func (s *Store) CreateShare(ctx context.Context, id int64, hash string, now, expires time.Time) (bool, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM location_shares WHERE expires<=?`, now.Unix()); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO location_shares(token_hash,device_id,expires)
	SELECT ?,?,? WHERE (SELECT privacy_mode FROM settings WHERE id=1)=0`, hash, id, expires.Unix())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n != 0, tx.Commit()
}
func (s *Store) Shares(ctx context.Context, id int64, now time.Time) (int, error) {
	var n int
	err := s.database.QueryRowContext(ctx, `SELECT count(*) FROM location_shares WHERE device_id=? AND expires>?`, id, now.Unix()).Scan(&n)
	return n, err
}
func (s *Store) RevokeShares(ctx context.Context, id int64) error {
	_, err := s.database.ExecContext(ctx, `DELETE FROM location_shares WHERE device_id=?`, id)
	return err
}

type SharedPosition struct {
	Latitude, Longitude float64
	Time                time.Time
	HasPosition         bool
	Icon, Kind          string
}

func (s *Store) SharedPosition(ctx context.Context, hash string, now time.Time) (SharedPosition, error) {
	var out SharedPosition
	var lat, lon sql.NullFloat64
	var stamp sql.NullString
	err := s.database.QueryRowContext(ctx, `
	SELECT p.latitude,p.longitude,p.tracker_time,coalesce(v.icon,'auto'),coalesce(v.kind,'other')
	FROM location_shares sh JOIN managed_devices d ON d.id=sh.device_id
	LEFT JOIN vehicle_profiles v ON v.device_id=d.id
	LEFT JOIN positions p ON p.id=(SELECT id FROM positions WHERE device_id=d.tracker_id AND gps_valid=1 ORDER BY julianday(tracker_time) DESC,id DESC LIMIT 1)
	WHERE sh.token_hash=? AND sh.expires>? AND (SELECT privacy_mode FROM settings WHERE id=1)=0`, hash, now.Unix()).
		Scan(&lat, &lon, &stamp, &out.Icon, &out.Kind)
	out.HasPosition = lat.Valid
	out.Latitude, out.Longitude = lat.Float64, lon.Float64
	out.Time, _ = time.Parse(time.RFC3339Nano, stamp.String)
	return out, err
}

func (s *Store) MonthlyEnabled(ctx context.Context, user int64) (bool, error) {
	var enabled bool
	err := s.database.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM monthly_reports WHERE user_id=?)`, user).Scan(&enabled)
	return enabled, err
}

func previousMonth(now time.Time) (time.Time, time.Time) {
	local := now.In(Warsaw)
	end := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, Warsaw)
	return end.AddDate(0, -1, 0), end
}
func (s *Store) SetMonthly(ctx context.Context, user int64, enabled bool, now time.Time) error {
	if !enabled {
		_, err := s.database.ExecContext(ctx, `DELETE FROM monthly_reports WHERE user_id=?`, user)
		return err
	}
	start, _ := previousMonth(now)
	_, err := s.database.ExecContext(ctx, `INSERT INTO monthly_reports(user_id,last_month) VALUES(?,?) ON CONFLICT DO NOTHING`, user, start.Format("2006-01"))
	return err
}
func (s *Store) MonthlyEvents(ctx context.Context, now time.Time) ([]ScheduledVehicleEvent, error) {
	start, end := previousMonth(now)
	month := start.Format("2006-01")
	rows, err := s.database.QueryContext(ctx, `SELECT user_id FROM monthly_reports WHERE last_month<?
	AND (SELECT privacy_mode FROM settings WHERE id=1)=0
	AND EXISTS (SELECT 1 FROM push_subscriptions WHERE user_id=monthly_reports.user_id)`, month)
	if err != nil {
		return nil, err
	}
	var users []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(users) == 0 {
		return nil, err
	}
	devices, err := s.ManagedDevices(ctx)
	if err != nil {
		return nil, err
	}
	var distance float64
	var moving time.Duration
	var count int
	for _, device := range devices {
		points, err := s.TrackPoints(ctx, device.ID, start, end)
		if err != nil {
			return nil, err
		}
		for _, t := range Trips(points) {
			distance += t.DistanceM
			moving += t.Moving
			count++
		}
	}
	text := fmt.Sprintf("Podsumowanie za %s. Przejazdy: %d · %.1f km · %d min jazdy (na podstawie dostępnych danych).", month, count, distance/1000, int(moving.Round(time.Minute).Minutes()))
	events := make([]ScheduledVehicleEvent, 0, len(users))
	for _, user := range users {
		events = append(events, ScheduledVehicleEvent{Month: month, AlertEvent: AlertEvent{UserID: user, Kind: "monthly", PlaceName: text}})
	}
	return events, nil
}
