package sqlite

import (
	"context"
	"time"
)

type TrackPoint struct {
	Time  time.Time `json:"time"`
	Lat   float64   `json:"lat"`
	Lon   float64   `json:"lon"`
	Speed float64   `json:"speed"`
}

type Trip struct {
	Points    []TrackPoint
	DistanceM float64
	Moving    time.Duration
	TopSpeed  float64
	Stop      time.Duration
}

func (t Trip) Start() time.Time { return t.Points[0].Time }
func (t Trip) End() time.Time   { return t.Points[len(t.Points)-1].Time }

const tripStop = 5 * time.Minute
const tripGap = 10 * time.Minute

// SegmentDistance filters GPS drift and jumps. It is shared by trip history
// and the estimated odometer.
func SegmentDistance(a, b TrackPoint) float64 {
	gap := b.Time.Sub(a.Time)
	if gap <= 0 || gap > tripGap || (a.Speed < 3 && b.Speed < 3) {
		return 0
	}
	meters := DistanceM(a.Lat, a.Lon, b.Lat, b.Lon)
	if meters < 5 || meters/gap.Seconds()*3.6 > 250 {
		return 0
	}
	return meters
}

func Trips(points []TrackPoint) []Trip {
	var out []Trip
	var current Trip
	var previous TrackPoint
	var still []TrackPoint
	stopIndex := -1
	finish := func() {
		if current.DistanceM >= 25 && len(current.Points) >= 2 {
			out = append(out, current)
		}
		current = Trip{}
		still = nil
	}
	for _, p := range points {
		gap := p.Time.Sub(previous.Time)
		if gap <= 0 {
			continue
		}
		if gap > tripGap {
			finish()
			previous = TrackPoint{}
			stopIndex = -1
		}
		if p.Speed < 3 && len(current.Points) == 0 {
			if stopIndex >= 0 {
				out[stopIndex].Stop = p.Time.Sub(out[stopIndex].End())
			}
			previous = p
			continue
		}
		stopIndex = -1
		if p.Speed < 3 && len(still) > 0 && p.Time.Sub(still[0].Time) >= tripStop {
			current.Stop = p.Time.Sub(still[0].Time)
			before := len(out)
			finish()
			if len(out) > before {
				stopIndex = len(out) - 1
			}
			previous = p
			continue
		}
		if len(current.Points) == 0 && !previous.Time.IsZero() && gap <= time.Minute {
			current.Points = append(current.Points, previous)
		}
		if len(current.Points) != 0 {
			d := SegmentDistance(previous, p)
			current.DistanceM += d
			if d > 0 && gap <= 2*time.Minute {
				current.Moving += gap
			}
		}
		if p.Speed < 3 {
			if len(still) == 0 {
				current.Points = append(current.Points, p)
			}
			still = append(still, p)
		} else {
			if len(still) > 1 {
				current.Points = append(current.Points, still[1:]...)
			}
			still = nil
			current.Points = append(current.Points, p)
			current.TopSpeed = max(current.TopSpeed, p.Speed)
		}
		previous = p
	}
	finish()
	return out
}

func (s *Store) TrackPoints(ctx context.Context, id int64, from, until time.Time) ([]TrackPoint, error) {
	rows, err := s.database.QueryContext(ctx, `
		SELECT unixepoch(p.tracker_time,'subsec'),p.latitude,p.longitude,p.speed_kph
		FROM positions p JOIN managed_devices d ON d.tracker_id=p.device_id
		WHERE d.id=? AND p.gps_valid=1 AND p.source='gprs'
		AND p.tracker_time>=? AND p.tracker_time<?
		AND (SELECT privacy_mode FROM settings WHERE id=1)=0
		ORDER BY 1,p.id`, id, from.UTC().Format("2006-01-02T15:04:05"), until.UTC().Format("2006-01-02T15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := []TrackPoint{}
	for rows.Next() {
		var p TrackPoint
		var stamp float64
		if err := rows.Scan(&stamp, &p.Lat, &p.Lon, &p.Speed); err != nil {
			return nil, err
		}
		p.Time = time.UnixMilli(int64(stamp*1000 + 0.5)).UTC()
		points = append(points, p)
	}
	return points, rows.Err()
}
