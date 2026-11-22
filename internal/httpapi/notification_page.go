package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed alert-settings.html
var notificationPage string

//go:embed assets/alert-settings.js
var notificationJS []byte

//go:embed assets/alert-settings.css
var notificationCSS []byte

//go:embed assets/sledz-na-tropie.webp
var detectiveIllustration []byte

func (s *Server) showNotifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentUser(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	devices, err := s.store.ManagedDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	settings, err := s.store.Settings(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.render(w, "alert-settings", http.StatusOK, map[string]any{"Devices": devices, "Privacy": settings.PrivacyMode})
}
