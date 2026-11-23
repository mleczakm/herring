package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RouteCursor continues a route query after the last position of the
// previous page. Positions are ordered by tracker time, then row ID, because
// one tracker time can appear twice (a frame and an SMS reply).
type RouteCursor struct {
	TrackerTime time.Time
	ID          int64
}

// RoutePositions returns up to limit valid GPS fixes of one tracker with
// tracker time in [from, to), oldest first, starting after cursor when it is
// not nil. Fixes without GPS are skipped: they repeat a stale position.
func (s *Store) RoutePositions(ctx context.Context, trackerID string, from, to time.Time, cursor *RouteCursor, limit int) ([]StoredPosition, error) {
	query := `
        SELECT id, device_id, tracker_time, received_at, gps_valid, latitude, longitude, speed_kph, heading, source
        FROM positions
        WHERE device_id = ? AND gps_valid = 1 AND tracker_time >= ? AND tracker_time < ?`
	args := []any{trackerID, formatTime(from), formatTime(to)}
	if cursor != nil {
		at := formatTime(cursor.TrackerTime)
		query += ` AND (tracker_time > ? OR (tracker_time = ? AND id > ?))`
		args = append(args, at, at, cursor.ID)
	}
	query += ` ORDER BY tracker_time, id LIMIT ?`
	args = append(args, limit)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query route: %w", err)
	}
	defer rows.Close()
	out := []StoredPosition{}
	for rows.Next() {
		var (
			p                     StoredPosition
			trackerTime, received string
		)
		if err := rows.Scan(&p.ID, &p.DeviceID, &trackerTime, &received, &p.GPSValid, &p.Latitude, &p.Longitude, &p.SpeedKPH, &p.Heading, &p.Source); err != nil {
			return nil, fmt.Errorf("scan route position: %w", err)
		}
		p.TrackerTime, _ = time.Parse(time.RFC3339Nano, trackerTime)
		p.ReceivedAt, _ = time.Parse(time.RFC3339Nano, received)
		out = append(out, p)
	}
	return out, rows.Err()
}

// OldestRouteTime reports the tracker time of the oldest valid fix kept for a
// tracker, so the history view can say how far back it reaches.
func (s *Store) OldestRouteTime(ctx context.Context, trackerID string) (time.Time, bool, error) {
	var oldest sql.NullString
	err := s.database.QueryRowContext(ctx, `SELECT MIN(tracker_time) FROM positions WHERE device_id = ? AND gps_valid = 1`, trackerID).Scan(&oldest)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query oldest route time: %w", err)
	}
	if !oldest.Valid {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, oldest.String)
	return t, err == nil, nil
}
