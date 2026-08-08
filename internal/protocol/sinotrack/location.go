// Package sinotrack decodes the textual H02 protocol used by SinoTrack ST-90x
// devices. It deliberately has no network or persistence dependencies.
package sinotrack

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMalformedFrame     = errors.New("malformed SinoTrack frame")
	ErrUnsupportedMessage = errors.New("unsupported SinoTrack message")
)

// Location is a decoded V1/V5/V6/V8-style location report.
type Location struct {
	Maker       string
	DeviceID    string
	MessageType string
	TrackerTime time.Time
	GPSValid    bool
	Latitude    float64
	Longitude   float64
	SpeedKPH    float64
	Heading     float64
	Status      string
	MCC         int
	MNC         int
	Raw         string
}

// ParseLocation decodes one complete, hash-terminated H02 location frame.
func ParseLocation(frame string) (Location, error) {
	var location Location
	frame = strings.TrimSpace(frame)
	if len(frame) < 3 || frame[0] != '*' || frame[len(frame)-1] != '#' {
		return location, fmt.Errorf("%w: invalid delimiters", ErrMalformedFrame)
	}

	fields := strings.Split(frame[1:len(frame)-1], ",")
	if len(fields) < 3 {
		return location, fmt.Errorf("%w: got %d fields, want at least 3", ErrMalformedFrame, len(fields))
	}
	if !isLocationType(fields[2]) {
		return location, fmt.Errorf("%w: type %q", ErrUnsupportedMessage, fields[2])
	}
	if len(fields) < 13 {
		return location, fmt.Errorf("%w: got %d fields, want at least 13", ErrMalformedFrame, len(fields))
	}
	if fields[4] != "A" && fields[4] != "V" {
		return location, fmt.Errorf("%w: invalid GPS validity %q", ErrMalformedFrame, fields[4])
	}

	trackerTime, err := time.ParseInLocation("020106150405", fields[11]+fields[3], time.UTC)
	if err != nil {
		return location, fmt.Errorf("%w: tracker time: %v", ErrMalformedFrame, err)
	}
	latitude, err := parseCoordinate(fields[5], fields[6], 90)
	if err != nil {
		return location, fmt.Errorf("%w: latitude: %v", ErrMalformedFrame, err)
	}
	longitude, err := parseCoordinate(fields[7], fields[8], 180)
	if err != nil {
		return location, fmt.Errorf("%w: longitude: %v", ErrMalformedFrame, err)
	}
	speedKnots, err := strconv.ParseFloat(strings.TrimSpace(fields[9]), 64)
	if err != nil || speedKnots < 0 {
		return location, fmt.Errorf("%w: invalid speed %q", ErrMalformedFrame, fields[9])
	}
	heading, err := strconv.ParseFloat(strings.TrimSpace(fields[10]), 64)
	if err != nil || heading < 0 || heading >= 360 {
		return location, fmt.Errorf("%w: invalid heading %q", ErrMalformedFrame, fields[10])
	}
	mcc, err := optionalNetworkCode(fields, 13)
	if err != nil {
		return location, fmt.Errorf("%w: invalid MCC: %v", ErrMalformedFrame, err)
	}
	mnc, err := optionalNetworkCode(fields, 14)
	if err != nil {
		return location, fmt.Errorf("%w: invalid MNC: %v", ErrMalformedFrame, err)
	}

	location = Location{
		Maker:       fields[0],
		DeviceID:    fields[1],
		MessageType: fields[2],
		TrackerTime: trackerTime,
		GPSValid:    fields[4] == "A",
		Latitude:    latitude,
		Longitude:   longitude,
		SpeedKPH:    math.Round(speedKnots*1.852*100) / 100,
		Heading:     heading,
		Status:      fields[12],
		MCC:         mcc,
		MNC:         mnc,
		Raw:         frame,
	}
	return location, nil
}

// Acknowledgement returns the V4 response expected by H02 trackers.
func (l Location) Acknowledgement(receivedAt time.Time) string {
	return fmt.Sprintf("*%s,%s,V4,%s,%s#", l.Maker, l.DeviceID, l.MessageType, receivedAt.UTC().Format("20060102150405"))
}

func isLocationType(messageType string) bool {
	if len(messageType) < 2 || messageType[0] != 'V' {
		return false
	}
	_, err := strconv.ParseUint(messageType[1:], 10, 8)
	return err == nil && messageType != "V4"
}

func parseCoordinate(value, hemisphere string, limit float64) (float64, error) {
	value = strings.TrimSpace(value)
	dot := strings.IndexByte(value, '.')
	if dot < 2 {
		return 0, fmt.Errorf("value %q has no minute precision", value)
	}
	degreeDigits := dot - 2
	degrees := 0.0
	if degreeDigits > 0 {
		var err error
		degrees, err = strconv.ParseFloat(value[:degreeDigits], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid degrees %q", value[:degreeDigits])
		}
	}
	minutes, err := strconv.ParseFloat(value[degreeDigits:], 64)
	if err != nil || minutes < 0 || minutes >= 60 {
		return 0, fmt.Errorf("invalid minutes %q", value[degreeDigits:])
	}

	coordinate := degrees + minutes/60
	switch hemisphere {
	case "N":
		if limit != 90 || coordinate > limit {
			return 0, fmt.Errorf("invalid northern coordinate")
		}
	case "S":
		if limit != 90 || coordinate > limit {
			return 0, fmt.Errorf("invalid southern coordinate")
		}
		coordinate = -coordinate
	case "E":
		if limit != 180 || coordinate > limit {
			return 0, fmt.Errorf("invalid eastern coordinate")
		}
	case "W":
		if limit != 180 || coordinate > limit {
			return 0, fmt.Errorf("invalid western coordinate")
		}
		coordinate = -coordinate
	default:
		return 0, fmt.Errorf("invalid hemisphere %q", hemisphere)
	}

	return math.Round(coordinate*1e6) / 1e6, nil
}

func optionalNetworkCode(fields []string, index int) (int, error) {
	if index >= len(fields) || strings.TrimSpace(fields[index]) == "" {
		return 0, nil
	}
	value := strings.TrimSpace(fields[index])
	base := 10
	if strings.IndexFunc(value, func(character rune) bool {
		return (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') &&
			(character < 'A' || character > 'F')
	}) >= 0 {
		return 0, fmt.Errorf("value %q is not decimal or hexadecimal", value)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')
	}) >= 0 {
		base = 16
	}
	parsed, err := strconv.ParseUint(value, base, 32)
	if err != nil {
		return 0, fmt.Errorf("value %q: %w", value, err)
	}
	return int(parsed), nil
}
