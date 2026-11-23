package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

// Resending the configuration costs one SMS per command, so a device can only
// be reconfigured again after its tracker had time to answer.
const configureInterval = 5 * time.Minute

const maxDeviceName = 80

// Messages shown after a device form redirects back to /urzadzenia.
var deviceNotices = map[string]string{
	"zapisano": "Zapisano zmiany.",
	"wyslano":  "Wysłaliśmy ustawienia ponownie. Czekamy na potwierdzenie z lokalizatora.",
	"usunieto": "Usunięto urządzenie i jego pozycje.",
}

type deviceView struct {
	sqlite.ManagedDevice
	Label, ModelLabel, StatusLabel, StatusClass string
}

type unknownTrackerView struct {
	ID, LastSeen, Attempts string
}

func (s *Server) showDevices(w http.ResponseWriter, r *http.Request) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	s.renderDevices(w, r, user, http.StatusOK, deviceNotices[r.URL.Query().Get("stan")], "")
}

func (s *Server) renderDevices(w http.ResponseWriter, r *http.Request, user sqlite.User, status int, notice, message string) {
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	unknown, err := s.unknownTrackerViews(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	views := make([]deviceView, 0, len(devices))
	for _, d := range devices {
		views = append(views, newDeviceView(d))
	}
	s.render(w, "devices", status, map[string]any{
		"User": user, "Devices": views, "Unknown": unknown, "Ready": s.sender != nil,
		"Active": "devices", "Privacy": settings.PrivacyMode, "Notice": notice, "Error": message,
	})
}

func newDeviceView(d sqlite.ManagedDevice) deviceView {
	v := deviceView{ManagedDevice: d, Label: d.Name, ModelLabel: "ST-901 2G"}
	if v.Label == "" {
		v.Label = "+" + d.PhoneNumber
	}
	if d.Model == "st901-4g" {
		v.ModelLabel = "ST-901 4G"
	}
	switch d.ConfigStatus {
	case "configured":
		v.StatusLabel, v.StatusClass = "Skonfigurowany", "ok"
	case "failed":
		v.StatusLabel, v.StatusClass = "Konfiguracja nieudana", "alert"
	default:
		v.StatusLabel, v.StatusClass = "Konfigurowanie…", "warn"
	}
	return v
}

func (s *Server) unknownTrackerViews(ctx context.Context) ([]unknownTrackerView, error) {
	now := time.Now()
	trackers, err := s.store.UnknownTrackers(ctx, now)
	if err != nil {
		return nil, err
	}
	out := make([]unknownTrackerView, 0, len(trackers))
	for _, t := range trackers {
		out = append(out, unknownTrackerView{ID: t.ID, LastSeen: timeAgo(now, t.LastSeen), Attempts: attemptsLabel(t.Attempts)})
	}
	return out, nil
}

// deviceForm checks the session and origin of a device form and parses it.
// It returns the device ID, or ok false after writing the response.
func (s *Server) deviceForm(w http.ResponseWriter, r *http.Request) (sqlite.User, int64, bool) {
	user, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", 401)
		return user, 0, false
	}
	if !s.sameOrigin(r) {
		http.Error(w, "invalid request origin", 403)
		return user, 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return user, 0, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return user, 0, false
	}
	return user, id, true
}

// parseTrackerID reads an optional tracker ID typed by the administrator.
func parseTrackerID(value string) (string, string) {
	if strings.TrimSpace(value) == "" {
		return "", ""
	}
	id, err := sqlite.NormalizeTrackerID(value)
	if err != nil {
		return "", "ID lokalizatora to od 5 do 20 cyfr. Znajdziesz je na naklejce lokalizatora."
	}
	return id, ""
}

func validDeviceName(name string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(name)) <= maxDeviceName
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	user, id, ok := s.deviceForm(w, r)
	if !ok {
		return
	}
	name := r.FormValue("name")
	if !validDeviceName(name) {
		s.renderDevices(w, r, user, 422, "", fmt.Sprintf("Nazwa może mieć najwyżej %d znaków.", maxDeviceName))
		return
	}
	trackerID, message := parseTrackerID(r.FormValue("tracker_id"))
	if message != "" {
		s.renderDevices(w, r, user, 422, "", message)
		return
	}
	switch err := s.store.UpdateManagedDevice(r.Context(), id, name, trackerID); {
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, r)
		return
	case errors.Is(err, sqlite.ErrTrackerInUse):
		s.renderDevices(w, r, user, 409, "", "To ID lokalizatora ma już inne urządzenie.")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.logger.Info("managed device updated", "device_id", id, "tracker_id", trackerID)
	redirectToDevice(w, r, "zapisano", id)
}

func (s *Server) reconfigureDevice(w http.ResponseWriter, r *http.Request) {
	user, id, ok := s.deviceForm(w, r)
	if !ok {
		return
	}
	if s.sender == nil {
		s.renderDevices(w, r, user, 503, "", "Wysyłanie SMS-ów nie jest skonfigurowane na serwerze.")
		return
	}
	device, err := s.store.ManagedDevice(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	last, sent, err := s.store.LastSMSCommand(r.Context(), id, "verify")
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if sent && time.Since(last) < configureInterval {
		w.Header().Set("Retry-After", strconv.Itoa(int(configureInterval.Seconds())))
		s.renderDevices(w, r, user, 429, "", "Ustawienia poszły przed chwilą. Poczekaj kilka minut na odpowiedź lokalizatora, zanim wyślesz je znowu.")
		return
	}
	if err := s.sendConfiguration(r.Context(), device); err != nil {
		s.logger.Error("could not resend tracker configuration", "device_id", id, "error", err)
		s.renderDevices(w, r, user, 502, "", "Nie udało się wysłać ustawień. Spróbuj ponownie za chwilę.")
		return
	}
	redirectToDevice(w, r, "wyslano", id)
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	user, id, ok := s.deviceForm(w, r)
	if !ok {
		return
	}
	if r.FormValue("confirm") != "1" {
		s.renderDevices(w, r, user, 422, "", "Zaznacz potwierdzenie, aby usunąć urządzenie.")
		return
	}
	switch err := s.store.DeleteManagedDevice(r.Context(), id); {
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, r)
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.logger.Info("managed device deleted", "device_id", id)
	http.Redirect(w, r, "/urzadzenia?stan=usunieto", 303)
}

func redirectToDevice(w http.ResponseWriter, r *http.Request, notice string, id int64) {
	http.Redirect(w, r, "/urzadzenia?stan="+notice+"#urzadzenie-"+strconv.FormatInt(id, 10), 303)
}

// sendConfiguration texts the ST-901 configuration commands to the device and
// records each one. The device ends up "awaiting_reply", or "failed" with the
// command that could not be sent.
func (s *Server) sendConfiguration(ctx context.Context, device sqlite.ManagedDevice) error {
	commands, err := s.config.Tracker.Commands()
	if err != nil {
		_ = s.store.SetConfigurationStatus(ctx, device.ID, "failed", "Niepełna konfiguracja serwera.")
		return err
	}
	if err := s.store.SetConfigurationStatus(ctx, device.ID, "sending", "Wysyłamy ustawienia."); err != nil {
		return err
	}
	for _, command := range commands {
		id, err := s.sender.Send(ctx, device.PhoneNumber, command.Body)
		if err != nil {
			_ = s.store.SetConfigurationStatus(ctx, device.ID, "failed", "Nie udało się wysłać komendy „"+command.Kind+"”.")
			return err
		}
		if err := s.store.RecordSMSCommand(ctx, device.ID, command.Kind, id, "accepted"); err != nil {
			return err
		}
	}
	return s.store.SetConfigurationStatus(ctx, device.ID, "awaiting_reply", "Wysłaliśmy ustawienia. Czekamy na potwierdzenie z lokalizatora.")
}

// timeAgo describes how long ago t was, in Polish.
func timeAgo(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "przed chwilą"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " min temu"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " godz. temu"
	case d < 48*time.Hour:
		return "wczoraj"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + " dni temu"
	}
}

// attemptsLabel counts rejected frames with the right Polish plural.
func attemptsLabel(n int64) string {
	word := "prób"
	switch {
	case n == 1:
		word = "próba"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		word = "próby"
	}
	return strconv.FormatInt(n, 10) + " " + word
}

// devicesJS only drives the mobile sidebar: the device forms work without
// JavaScript.
const devicesJS = `(function(){
"use strict";
var sidebar = document.getElementById("sidebar");
var backdrop = document.getElementById("sidebar-backdrop");
var menuToggle = document.getElementById("menu-toggle");
if (!sidebar || !backdrop || !menuToggle) return;
menuToggle.addEventListener("click", function(){
  sidebar.classList.toggle("open");
  backdrop.classList.toggle("open");
  menuToggle.setAttribute("aria-expanded", String(sidebar.classList.contains("open")));
});
backdrop.addEventListener("click", function(){
  sidebar.classList.remove("open"); backdrop.classList.remove("open"); menuToggle.setAttribute("aria-expanded", "false");
});
})();
`
