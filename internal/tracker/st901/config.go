// Package st901 builds the documented SMS configuration commands for ST-901 trackers.
package st901

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type Profile struct {
	Password, ControlNumber, APN, ServerHost    string
	ServerPort, MovingInterval, StoppedInterval int
}

type Command struct{ Kind, Body string }

var phoneDigits = regexp.MustCompile(`^[0-9]{7,15}$`)

func NormalizePhone(value string) (string, error) {
	value = strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(value)
	if !phoneDigits.MatchString(value) {
		return "", fmt.Errorf("phone number must contain 7 to 15 digits")
	}
	return value, nil
}

func (p Profile) Commands() ([]Command, error) {
	password := p.Password
	if password == "" {
		password = "0000"
	}
	if len(password) != 4 || strings.Trim(password, "0123456789") != "" {
		return nil, fmt.Errorf("tracker password must contain four digits")
	}
	control, err := NormalizePhone(p.ControlNumber)
	if err != nil {
		return nil, fmt.Errorf("control number: %w", err)
	}
	if p.APN == "" || strings.ContainsAny(p.APN, " \r\n") {
		return nil, fmt.Errorf("APN must be a single non-empty value")
	}
	if net.ParseIP(p.ServerHost) == nil && !validHostname(p.ServerHost) {
		return nil, fmt.Errorf("invalid tracker server host")
	}
	if p.ServerPort < 1 || p.ServerPort > 65535 {
		return nil, fmt.Errorf("invalid tracker server port")
	}
	if p.MovingInterval < 10 || p.MovingInterval > 18000 || p.StoppedInterval < 10 || p.StoppedInterval > 18000 {
		return nil, fmt.Errorf("tracker intervals must be between 10 and 18000 seconds")
	}
	return []Command{
		{"control_number", control + password + " 1"},
		{"apn", "803" + password + " " + p.APN},
		{"server", "804" + password + " " + p.ServerHost + " " + strconv.Itoa(p.ServerPort)},
		{"moving_interval", "805" + password + " " + strconv.Itoa(p.MovingInterval)},
		{"stopped_interval", "809" + password + " " + strconv.Itoa(p.StoppedInterval)},
		{"gprs_mode", "710" + password},
		{"verify", "RCONF"},
	}, nil
}

func validHostname(value string) bool {
	if len(value) < 1 || len(value) > 253 || strings.ContainsAny(value, " \r\n") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// ConfigurationMatches deliberately requires the settings that prove TCP telemetry can reach Herring.
func ConfigurationMatches(reply string, p Profile) bool {
	u := strings.ToUpper(strings.ReplaceAll(reply, " ", ""))
	return strings.Contains(u, "MODE:GPRS") && strings.Contains(u, "APN:"+strings.ToUpper(p.APN)) &&
		strings.Contains(u, strings.ToUpper(p.ServerHost)+":"+strconv.Itoa(p.ServerPort))
}
