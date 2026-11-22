package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mleczakm/herring/internal/push"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

type pushStore interface {
	SavePushSubscription(rctx context.Context, userID int64, sub sqlite.PushSubscription) error
	DeletePushSubscription(rctx context.Context, userID int64, endpoint string) error
}

// RegisterPush installs optional notification endpoints on the same authenticated server.
func (s *Server) RegisterPush(mux *http.ServeMux, service *push.Service) {
	mux.HandleFunc("GET /api/push/config", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUser(r); !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		key := ""
		if service != nil {
			key = service.PublicKey()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": service != nil, "publicKey": key})
	})
	mux.HandleFunc("POST /api/push/subscribe", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.currentUser(r)
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		if !pushOriginOK(s, r) {
			http.Error(w, "invalid request origin", 403)
			return
		}
		if service == nil {
			http.Error(w, "push is not configured", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var data struct {
			Endpoint string `json:"endpoint"`
			Keys     struct {
				P256DH string `json:"p256dh"`
				Auth   string `json:"auth"`
			} `json:"keys"`
		}
		if json.NewDecoder(r.Body).Decode(&data) != nil {
			http.Error(w, "invalid subscription", 400)
			return
		}
		sub := sqlite.PushSubscription{Endpoint: data.Endpoint, P256DH: data.Keys.P256DH, Auth: data.Keys.Auth}
		if push.ValidateSubscription(sub) != nil {
			http.Error(w, "invalid subscription", 400)
			return
		}
		if err := s.store.SavePushSubscription(r.Context(), user.ID, sub); err != nil {
			s.internalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/push/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.currentUser(r)
		if !ok {
			http.Error(w, "unauthorized", 401)
			return
		}
		if !pushOriginOK(s, r) {
			http.Error(w, "invalid request origin", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var data struct {
			Endpoint string `json:"endpoint"`
		}
		if json.NewDecoder(r.Body).Decode(&data) != nil || len(data.Endpoint) > 2048 || data.Endpoint == "" {
			http.Error(w, "invalid subscription", 400)
			return
		}
		if err := s.store.DeletePushSubscription(r.Context(), user.ID, data.Endpoint); err != nil {
			s.internalError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
func pushOriginOK(s *Server, r *http.Request) bool {
	return r.Header.Get("Origin") != "" && !strings.EqualFold(r.Header.Get("Origin"), "null") && s.sameOrigin(r)
}

// PushWorkerJS is appended to the existing same-origin service worker.
const PushWorkerJS = `
self.addEventListener('push', event => {
  let data = {};
  try { data = event.data ? event.data.json() : {}; } catch (_) {}
  const title = typeof data.title === 'string' ? data.title : 'Śledź';
  const body = typeof data.body === 'string' ? data.body : 'Nowy alert lokalizatora.';
  event.waitUntil(self.registration.showNotification(title, {body, icon:'/assets/icon-192.png?v=2', badge:'/assets/icon-192.png?v=2', data:{url:'/'}}));
});
self.addEventListener('notificationclick', event => {
  event.notification.close();
  event.waitUntil(clients.matchAll({type:'window',includeUncontrolled:true}).then(windows => {
    for (const window of windows) { if (new URL(window.url).origin === self.location.origin) return window.focus(); }
    return clients.openWindow('/');
  }));
});
`
