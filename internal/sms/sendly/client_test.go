package sendly

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message_id":"sms-1"}`))
	}))
	defer server.Close()
	id, err := (&Client{Token: "secret", From: "+48500100200", BaseURL: server.URL}).Send(context.Background(), "+48500600700", "RCONF")
	if err != nil || id != "sms-1" {
		t.Fatalf("Send = (%q, %v)", id, err)
	}
}
