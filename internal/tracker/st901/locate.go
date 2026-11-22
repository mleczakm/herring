package st901

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LocateCommand asks the tracker to answer by SMS with its position. ST-901
// manuals document it as "669" followed by the SMS password. The reply goes to
// the control number, which Herring sets to its own SMS gateway number.
func (p Profile) LocateCommand() (Command, error) {
	password := p.Password
	if password == "" {
		password = "0000"
	}
	if len(password) != 4 || strings.Trim(password, "0123456789") != "" {
		return Command{}, fmt.Errorf("tracker password must contain four digits")
	}
	return Command{Kind: "locate", Body: "669" + password}, nil
}

// LocationReply is a position decoded from the tracker's SMS answer.
type LocationReply struct {
	Latitude, Longitude float64
	SpeedKPH            float64
	GPSValid            bool
	TrackerID           string
	// TrackerTime comes from the Date/Time fields when they agree with the
	// receive time; otherwise it is the receive minute. The firmware's time zone
	// is not documented, and a clock set to local time must not make an SMS
	// position look hours newer than real GPRS positions.
	TrackerTime time.Time
	Battery     int
}

var (
	// Manuals show "http://maps.google.com/maps?q=+22.64208,+114.18828"; some
	// firmware writes hemisphere letters instead of signs.
	replyLink = regexp.MustCompile(`(?i)[?&]q=\s*([NS+-]?)\s*(\d{1,2}(?:\.\d+)?)\s*,\s*([EW+-]?)\s*(\d{1,3}(?:\.\d+)?)`)
	replyLat  = regexp.MustCompile(`(?i)\blat(?:itude)?\s*[:=]\s*([NS+-]?)\s*(\d{1,2}\.\d+)\s*([NS]?)`)
	replyLon  = regexp.MustCompile(`(?i)\blon(?:g|gitude)?\s*[:=]\s*([EW+-]?)\s*(\d{1,3}\.\d+)\s*([EW]?)`)
	replyDate = regexp.MustCompile(`(?i)\bdate\s*:\s*(\d{4})-(\d{1,2})-(\d{1,2})`)
	replyTime = regexp.MustCompile(`(?i)\btime\s*:\s*(\d{1,2}):(\d{2})(?::(\d{2}))?`)
	replySpd  = regexp.MustCompile(`(?i)\b(?:speed|spd)\s*:\s*(\d+(?:\.\d+)?)\s*(?:km/?h)?`)
	replyFix  = regexp.MustCompile(`(?i)\bfix\s*:\s*([AV])\b`)
	replyID   = regexp.MustCompile(`(?i)\bID\s*:\s*(\d{6,20})\b`)
	replyBat  = regexp.MustCompile(`(?i)\bbat\s*:\s*(\d{1,3})\b`)
)

// ParseLocationReply decodes a location SMS. It reports false for anything
// that is not a usable position, including configuration replies.
func ParseLocationReply(body string, received time.Time) (LocationReply, bool) {
	var reply LocationReply
	lat, lon, ok := replyCoordinates(body)
	if !ok {
		return reply, false
	}
	reply.Latitude, reply.Longitude = lat, lon
	reply.GPSValid = true
	if m := replyFix.FindStringSubmatch(body); m != nil {
		reply.GPSValid = strings.EqualFold(m[1], "A")
	}
	if m := replySpd.FindStringSubmatch(body); m != nil {
		reply.SpeedKPH, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := replyID.FindStringSubmatch(body); m != nil {
		reply.TrackerID = m[1]
	}
	reply.Battery = -1
	if m := replyBat.FindStringSubmatch(body); m != nil {
		reply.Battery, _ = strconv.Atoi(m[1])
	}
	reply.TrackerTime = replyTimestamp(body, received)
	return reply, true
}

func replyCoordinates(body string) (float64, float64, bool) {
	if m := replyLink.FindStringSubmatch(body); m != nil {
		lat, err1 := signedDegrees(m[1], m[2], "", "S")
		lon, err2 := signedDegrees(m[3], m[4], "", "W")
		return lat, lon, err1 == nil && err2 == nil && plausible(lat, lon)
	}
	latMatch, lonMatch := replyLat.FindStringSubmatch(body), replyLon.FindStringSubmatch(body)
	if latMatch == nil || lonMatch == nil {
		return 0, 0, false
	}
	lat, err1 := signedDegrees(latMatch[1], latMatch[2], latMatch[3], "S")
	lon, err2 := signedDegrees(lonMatch[1], lonMatch[2], lonMatch[3], "W")
	return lat, lon, err1 == nil && err2 == nil && plausible(lat, lon)
}

func signedDegrees(prefix, value, suffix, negative string) (float64, error) {
	degrees, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	for _, marker := range []string{prefix, suffix} {
		if marker == "-" || strings.EqualFold(marker, negative) {
			degrees = -degrees
		}
	}
	return degrees, nil
}

// plausible rejects out-of-range values and the 0,0 a tracker without any fix
// may report.
func plausible(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && (lat != 0 || lon != 0)
}

func replyTimestamp(body string, received time.Time) time.Time {
	date, clock := replyDate.FindStringSubmatch(body), replyTime.FindStringSubmatch(body)
	if date != nil && clock != nil {
		year, _ := strconv.Atoi(date[1])
		month, _ := strconv.Atoi(date[2])
		day, _ := strconv.Atoi(date[3])
		hour, _ := strconv.Atoi(clock[1])
		minute, _ := strconv.Atoi(clock[2])
		second := 0
		if clock[3] != "" {
			second, _ = strconv.Atoi(clock[3])
		}
		t := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
		// time.Date normalises invalid values; accept only a faithful round trip
		// that is plausible for an SMS: read at most 30 minutes before receipt.
		if t.Day() == day && int(t.Month()) == month && hour < 24 && minute < 60 && second < 60 &&
			!t.Before(received.Add(-30*time.Minute)) && !t.After(received.Add(5*time.Minute)) {
			return t
		}
	}
	// Without a usable timestamp, the receive minute keeps repeated webhook
	// deliveries of the same SMS from creating duplicate positions.
	return received.UTC().Truncate(time.Minute)
}
