package httpapi

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

type vehicleOption struct{ Value, Label, Symbol string }

var vehicleTypes = []vehicleOption{
	{"car", "Samochód", "🚗"}, {"bike", "Rower", "🚲"}, {"motorcycle", "Motocykl", "🏍️"},
	{"scooter", "Hulajnoga", "🛴"}, {"van", "Bus", "🚐"}, {"truck", "Ciężarówka", "🚚"},
	{"trailer", "Przyczepa", "🛞"}, {"boat", "Łódź", "⛵"}, {"other", "Inny", "●"},
}
var vehicleIcons = append([]vehicleOption{{"auto", "Według typu", ""}, {"pin", "Pinezka", "📍"}, {"dot", "Kropka", "●"}}, vehicleTypes[:len(vehicleTypes)-1]...)

func validOption(options []vehicleOption, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func (s *Server) vehicleRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /pojazdy", s.vehicles)
	m.HandleFunc("GET /pojazdy/{id}", s.vehicles)
	m.HandleFunc("POST /pojazdy/{id}", s.updateVehicle)
	m.HandleFunc("GET /trasy", s.tripHistory)
	m.HandleFunc("POST /trasy/notatka", s.tripNote)
	m.HandleFunc("POST /trasy/podsumowanie", s.monthlySettings)
	m.HandleFunc("GET /lokalizacja/{token}", s.sharedMap)
	m.HandleFunc("GET /api/lokalizacja/{token}", s.sharedFix)
	m.HandleFunc("GET /assets/private-trips.js", s.asset("text/javascript; charset=utf-8", []byte(privateTripsJS)))
	m.HandleFunc("GET /assets/vehicle-maps.js", s.asset("text/javascript; charset=utf-8", []byte(vehicleMapsJS)))
}

type vehiclePageData struct {
	Active          string
	Privacy         bool
	Devices         []sqlite.ManagedDevice
	Device          sqlite.ManagedDevice
	Profile         sqlite.VehicleProfile
	Reminders       []sqlite.Reminder
	Guard           sqlite.Guard
	Shares          int
	ShareURL        string
	Types, Icons    []vehicleOption
	Monthly         bool
	From, To        string
	Trips           []tripView
	ByDay           []tripDay
	ByMonth         []tripDay
	Stops           []tripStopView
	FromMS, UntilMS int64
	Km              float64
	Minutes         int
	Issue           string
}
type tripView struct {
	Key, Start, End, Note string
	StopMinutes           int
	Km                    float64
	Minutes               int
	Top                   float64
}
type tripStopView struct {
	Label          string
	Count, Minutes int
}
type tripDay struct {
	Day            string
	Count, Minutes int
	Km             float64
}

func (s *Server) vehiclePage(w http.ResponseWriter, r *http.Request, active string) (*vehiclePageData, sqlite.User, bool) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, u, false
	}
	w.Header().Set("Cache-Control", "no-store")
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return nil, u, false
	}
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return nil, u, false
	}
	return &vehiclePageData{Active: active, Privacy: settings.PrivacyMode, Devices: devices, Types: vehicleTypes, Icons: vehicleIcons}, u, true
}

func (s *Server) selectVehicle(w http.ResponseWriter, r *http.Request, data *vehiclePageData, raw string) bool {
	if raw == "" && len(data.Devices) > 0 {
		data.Device = data.Devices[0]
		return true
	}
	if raw == "" {
		return true
	}
	id, _ := strconv.ParseInt(raw, 10, 64)
	for _, d := range data.Devices {
		if id == d.ID {
			data.Device = d
			return true
		}
	}
	http.NotFound(w, r)
	return false
}

func (s *Server) fillVehicle(w http.ResponseWriter, r *http.Request, data *vehiclePageData) bool {
	id := data.Device.ID
	if id == 0 {
		return true
	}
	load := []func() error{
		func() (err error) { data.Profile, err = s.store.VehicleProfile(r.Context(), id); return },
		func() (err error) { data.Reminders, err = s.store.Reminders(r.Context(), id); return },
		func() (err error) { data.Guard, err = s.store.Guard(r.Context(), id); return },
		func() (err error) { data.Shares, err = s.store.Shares(r.Context(), id, time.Now()); return },
	}
	for _, f := range load {
		if err := f(); err != nil {
			s.internalError(w, r, err)
			return false
		}
	}
	return true
}

func (s *Server) vehicles(w http.ResponseWriter, r *http.Request) {
	data, _, ok := s.vehiclePage(w, r, "vehicles")
	if ok && s.selectVehicle(w, r, data, r.PathValue("id")) && s.fillVehicle(w, r, data) {
		s.render(w, "vehicles", 200, data)
	}
}
func (s *Server) vehicleForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") == "null" || !s.sameOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Niedozwolone źródło żądania.", 403)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Nieprawidłowy formularz.", 400)
		return false
	}
	return true
}

func numericField(raw string, min, max float64) (float64, bool) {
	n, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), 64)
	return n, err == nil && n >= min && n <= max
}

func (s *Server) updateVehicle(w http.ResponseWriter, r *http.Request) {
	if !s.vehicleForm(w, r) {
		return
	}
	data, u, ok := s.vehiclePage(w, r, "vehicles")
	if !ok || !s.selectVehicle(w, r, data, r.PathValue("id")) {
		return
	}
	id, now, ctx := data.Device.ID, time.Now(), r.Context()
	var err error
	switch r.FormValue("action") {
	case "profile":
		kind, icon := r.FormValue("kind"), r.FormValue("icon")
		if !validOption(vehicleTypes, kind) || !validOption(vehicleIcons, icon) {
			data.Issue = "Wybierz typ pojazdu i ikonę z listy."
			break
		}
		err = s.store.SaveVehicleProfile(ctx, id, kind, icon, nil)
	case "odometer":
		km, valid := numericField(r.FormValue("odometer"), 0, 10000000)
		if !valid {
			data.Issue = "Wpisz poprawny przebieg w kilometrach."
			break
		}
		profile, e := s.store.VehicleProfile(ctx, id)
		if e != nil {
			err = e
			break
		}
		meters := km * 1000
		err = s.store.SaveVehicleProfile(ctx, id, profile.Kind, profile.Icon, &meters)
	case "add-reminder":
		title, date := strings.TrimSpace(r.FormValue("title")), r.FormValue("date")
		km, valid := numericField(r.FormValue("km"), 0.001, 10000000)
		if r.FormValue("km") == "" {
			km, valid = 0, true
		}
		if date != "" {
			_, e := time.Parse(time.DateOnly, date)
			valid = valid && e == nil
		}
		if title == "" || len([]rune(title)) > 100 || !valid || (date == "" && km == 0) {
			data.Issue = "Podaj nazwę i co najmniej jeden próg: datę lub przebieg."
			break
		}
		err = s.store.AddReminder(ctx, id, u.ID, title, date, km*1000)
	case "delete-reminder":
		reminder, _ := strconv.ParseInt(r.FormValue("reminder"), 10, 64)
		err = s.store.DeleteReminder(ctx, id, reminder)
	case "arm":
		radius, valid := numericField(r.FormValue("radius"), 30, 2000)
		if !valid {
			data.Issue = "Podaj promień od 30 do 2000 m."
			break
		}
		var armed bool
		armed, err = s.store.SetGuard(ctx, id, u.ID, radius, now)
		if !armed {
			data.Issue = "Do uzbrojenia potrzebna jest ważna pozycja GPS stojącego pojazdu z ostatnich 10 minut oraz wyłączony tryb prywatny."
		}
	case "disarm":
		_, err = s.store.SetGuard(ctx, id, u.ID, 0, now)
	case "share":
		minutes, valid := numericField(r.FormValue("minutes"), 5, 1440)
		if !valid {
			data.Issue = "Wybierz ważność linku od 5 minut do 24 godzin."
			break
		}
		token := randomToken()
		var created bool
		created, err = s.store.CreateShare(ctx, id, tokenHash(token), now, now.Add(time.Duration(minutes*float64(time.Minute))))
		if created {
			data.ShareURL = "/lokalizacja/" + token
		} else {
			data.Issue = "Udostępnianie jest dostępne przy wyłączonym trybie prywatnym."
		}
	case "revoke":
		err = s.store.RevokeShares(ctx, id)
	default:
		http.Error(w, "Nieznana operacja.", 400)
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if data.ShareURL != "" || data.Issue != "" {
		status := 200
		if data.Issue != "" {
			status = 400
		}
		if s.fillVehicle(w, r, data) {
			s.render(w, "vehicles", status, data)
		}
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/pojazdy/%d", id), http.StatusSeeOther)
}

func tripRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	local := now.In(sqlite.Warsaw)
	if from == "" {
		from = local.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	if to == "" {
		to = local.Format(time.DateOnly)
	}
	start, e1 := time.ParseInLocation(time.DateOnly, from, sqlite.Warsaw)
	end, e2 := time.ParseInLocation(time.DateOnly, to, sqlite.Warsaw)
	if e1 != nil || e2 != nil || end.Before(start) || end.After(start.AddDate(0, 0, 92)) {
		return time.Time{}, time.Time{}, errors.New("Wybierz poprawny zakres do 93 dni.")
	}
	return start, end.AddDate(0, 0, 1), nil
}

func (s *Server) tripHistory(w http.ResponseWriter, r *http.Request) {
	data, u, ok := s.vehiclePage(w, r, "trips")
	if !ok || !s.selectVehicle(w, r, data, r.URL.Query().Get("id")) {
		return
	}
	var err error
	data.Monthly, err = s.store.MonthlyEnabled(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	from, until, err := tripRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"), time.Now())
	if err != nil {
		data.Issue = err.Error()
		s.render(w, "trips", 400, data)
		return
	}
	data.From, data.To = from.Format(time.DateOnly), until.AddDate(0, 0, -1).Format(time.DateOnly)
	data.FromMS, data.UntilMS = from.UnixMilli(), until.UnixMilli()
	if data.Device.ID == 0 || data.Privacy {
		s.render(w, "trips", 200, data)
		return
	}
	points, err := s.store.TrackPoints(r.Context(), data.Device.ID, from, until)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	trips := sqlite.Trips(points)
	notes, err := s.store.TripNotes(r.Context(), data.Device.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	days := map[string]int{}
	months := map[string]int{}
	stops := map[string]tripStopView{}
	places, err := s.store.Places(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var moving time.Duration
	for i := len(trips) - 1; i >= 0; i-- {
		t := trips[i]
		key := t.Start().UTC().Format("2006-01-02T15:04:05.000Z")
		view := tripView{Key: key, Start: t.Start().In(sqlite.Warsaw).Format("02.01.2006 15:04"), End: t.End().In(sqlite.Warsaw).Format("02.01.2006 15:04"), Note: notes[key], Km: t.DistanceM / 1000, Minutes: int(t.Moving.Round(time.Minute).Minutes()), Top: t.TopSpeed, StopMinutes: int(t.Stop.Minutes())}
		day := t.Start().In(sqlite.Warsaw).Format(time.DateOnly)
		idx, exists := days[day]
		if !exists {
			idx = len(data.ByDay)
			days[day] = idx
			data.ByDay = append(data.ByDay, tripDay{Day: day})
		}
		stats := &data.ByDay[idx]
		stats.Count++
		stats.Km += view.Km
		stats.Minutes += view.Minutes
		month := day[:7]
		idx, exists = months[month]
		if !exists {
			idx = len(data.ByMonth)
			months[month] = idx
			data.ByMonth = append(data.ByMonth, tripDay{Day: month})
		}
		monthStats := &data.ByMonth[idx]
		monthStats.Count++
		monthStats.Km += view.Km
		monthStats.Minutes += view.Minutes
		if view.StopMinutes > 0 {
			p := t.Points[len(t.Points)-1]
			label := fmt.Sprintf("%.3f, %.3f", p.Lat, p.Lon)
			for _, place := range places {
				if sqlite.DistanceM(p.Lat, p.Lon, place.Latitude, place.Longitude) <= math.Max(place.RadiusM, 30) {
					label = place.Name
					break
				}
			}
			stop := stops[label]
			stop.Label = label
			stop.Count++
			stop.Minutes += view.StopMinutes
			stops[label] = stop
		}
		data.Km += view.Km
		moving += t.Moving
		data.Trips = append(data.Trips, view)
		if r.URL.Query().Get("trip") == key {
			switch r.URL.Query().Get("export") {
			case "gpx":
				writeGPX(w, t)
				return
			case "points":
				alertJSON(w, t.Points)
				return
			}
		}
	}
	data.Minutes = int(moving.Round(time.Minute).Minutes())
	for _, stop := range stops {
		data.Stops = append(data.Stops, stop)
	}
	sort.Slice(data.Stops, func(i, j int) bool {
		a, b := data.Stops[i], data.Stops[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Minutes != b.Minutes {
			return a.Minutes > b.Minutes
		}
		return a.Label < b.Label
	})
	if len(data.Stops) > 10 {
		data.Stops = data.Stops[:10]
	}
	if r.URL.Query().Get("export") == "csv" {
		writeTripCSV(w, data.Trips)
		return
	}
	if r.URL.Query().Get("export") != "" {
		http.NotFound(w, r)
		return
	}
	s.render(w, "trips", 200, data)
}

func (s *Server) tripNote(w http.ResponseWriter, r *http.Request) {
	if !s.vehicleForm(w, r) {
		return
	}
	data, _, ok := s.vehiclePage(w, r, "trips")
	if !ok || !s.selectVehicle(w, r, data, r.FormValue("id")) {
		return
	}
	start, err := time.Parse(time.RFC3339Nano, r.FormValue("trip"))
	note := strings.TrimSpace(r.FormValue("note"))
	from, until, rangeErr := tripRange(r.FormValue("from"), r.FormValue("to"), time.Now())
	if err != nil || rangeErr != nil || len([]rune(note)) > 300 || data.Device.ID == 0 || data.Privacy {
		http.Error(w, "Nie można zapisać notatki.", 400)
		return
	}
	points, err := s.store.TrackPoints(r.Context(), data.Device.ID, from, until)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, t := range sqlite.Trips(points) {
		if t.Start().Equal(start) {
			if err = s.store.SaveTripNote(r.Context(), data.Device.ID, start, note); err != nil {
				s.internalError(w, r, err)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/trasy?id=%d&from=%s&to=%s", data.Device.ID, from.Format(time.DateOnly), until.AddDate(0, 0, -1).Format(time.DateOnly)), 303)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) monthlySettings(w http.ResponseWriter, r *http.Request) {
	if !s.vehicleForm(w, r) {
		return
	}
	_, u, ok := s.vehiclePage(w, r, "trips")
	if !ok {
		return
	}
	if err := s.store.SetMonthly(r.Context(), u.ID, r.FormValue("enabled") == "on", time.Now()); err != nil {
		s.internalError(w, r, err)
		return
	}
	http.Redirect(w, r, "/trasy", 303)
}

func writeTripCSV(w http.ResponseWriter, trips []tripView) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="przejazdy.csv"`)
	c := csv.NewWriter(w)
	_ = c.Write([]string{"Początek (Europe/Warsaw)", "Koniec (Europe/Warsaw)", "Dystans (km)", "Czas ruchu (min)", "Maks. prędkość (km/h)", "Notatka"})
	for _, t := range trips {
		note := strings.TrimLeft(t.Note, " \r\n\t")
		if note != "" && strings.ContainsRune("=+-@", rune(note[0])) {
			note = "'" + note
		}
		_ = c.Write([]string{t.Start, t.End, fmt.Sprintf("%.3f", t.Km), strconv.Itoa(t.Minutes), fmt.Sprintf("%.1f", t.Top), note})
	}
	c.Flush()
}

func writeGPX(w http.ResponseWriter, trip sqlite.Trip) {
	type point struct {
		Lat  float64   `xml:"lat,attr"`
		Lon  float64   `xml:"lon,attr"`
		Time time.Time `xml:"time"`
	}
	doc := struct {
		XMLName xml.Name `xml:"gpx"`
		XMLNS   string   `xml:"xmlns,attr"`
		Version string   `xml:"version,attr"`
		Creator string   `xml:"creator,attr"`
		Points  []point  `xml:"trk>trkseg>trkpt"`
	}{XMLNS: "http://www.topografix.com/GPX/1/1", Version: "1.1", Creator: "Śledź"}
	for _, p := range trip.Points {
		doc.Points = append(doc.Points, point{p.Lat, p.Lon, p.Time})
	}
	w.Header().Set("Content-Type", "application/gpx+xml")
	w.Header().Set("Content-Disposition", `attachment; filename="przejazd.gpx"`)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(doc)
}

func (s *Server) loadShare(w http.ResponseWriter, r *http.Request) (sqlite.SharedPosition, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	p, err := s.store.SharedPosition(r.Context(), tokenHash(r.PathValue("token")), time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Link wygasł lub został odwołany.", 404)
		return p, false
	}
	if err != nil {
		s.logger.Error("read location share", "error", err)
		http.Error(w, "Nie udało się pobrać lokalizacji.", 500)
		return p, false
	}
	return p, true
}

func (s *Server) sharedMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.loadShare(w, r); ok {
		s.render(w, "share-map", 200, nil)
	}
}
func (s *Server) sharedFix(w http.ResponseWriter, r *http.Request) {
	if p, ok := s.loadShare(w, r); ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p)
	}
}
