package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

func TestValidateSubscription(t *testing.T) {
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := sqlite.PushSubscription{Endpoint: "https://fcm.googleapis.com/fcm/send/token", P256DH: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16))}
	if err := ValidateSubscription(sub); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://fcm.googleapis.com/x", "https://fcm.googleapis.com.evil.example/x", "https://fcm.googleapis.com@127.0.0.1/x", "https://fcm.googleapis.com:444/x", "https://127.0.0.1/x"} {
		bad := sub
		bad.Endpoint = endpoint
		if ValidateSubscription(bad) == nil {
			t.Errorf("accepted %q", endpoint)
		}
	}
	bad := sub
	bad.P256DH = "bad"
	if ValidateSubscription(bad) == nil {
		t.Fatal("accepted invalid key")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestHumorPromptContainsOnlyGenericCategory(t *testing.T) {
	svc := &Service{config: Config{GeminiKey: "test", GeminiModel: "gemini-3.5-flash-lite"}, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		body := string(b)
		for _, secret := range []string{"SecretDevice", "SecretPlace", "52.123", "21.456"} {
			if strings.Contains(body, secret) {
				t.Fatalf("unexpected identifying data in prompt")
			}
		}
		if !strings.Contains(body, "przybyciu") {
			t.Fatalf("missing category in prompt: %s", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"parts":[{"text":"Śledź dopłynął!"}]}}]}`)), Header: make(http.Header)}, nil
	})}}
	if got := svc.humor(context.Background(), "entry"); got != "Śledź dopłynął!" {
		t.Fatalf("unexpected response %q", got)
	}
}

func TestGeneratedVAPIDKeysPersist(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := New(store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(store, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey() == "" || first.PublicKey() != second.PublicKey() {
		t.Fatal("VAPID key not persisted")
	}
	if _, err = New(store, Config{PublicKey: first.PublicKey()}); err == nil {
		t.Fatal("accepted incomplete configured key pair")
	}
}

type deliveryStore struct {
	subs    map[int64][]sqlite.PushSubscription
	privacy bool
	deleted []string
}

func (s *deliveryStore) PushSubscriptions(_ context.Context, id int64) ([]sqlite.PushSubscription, error) {
	return s.subs[id], nil
}
func (s *deliveryStore) DeletePushSubscription(_ context.Context, id int64, endpoint string) error {
	s.deleted = append(s.deleted, fmt.Sprintf("%d:%s", id, endpoint))
	return nil
}
func (s *deliveryStore) Settings(context.Context) (sqlite.Settings, error) {
	return sqlite.Settings{PrivacyMode: s.privacy}, nil
}
func (s *deliveryStore) EnsureVAPIDKeys(_ context.Context, priv, pub string) (string, string, error) {
	return priv, pub, nil
}

func TestSendEncryptedVAPIDAndPrivacy(t *testing.T) {
	_, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := sqlite.PushSubscription{Endpoint: "https://fcm.googleapis.com/fcm/send/secret-token", P256DH: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), x, y)), Auth: base64.RawURLEncoding.EncodeToString(make([]byte, 16))}
	store := &deliveryStore{subs: map[int64][]sqlite.PushSubscription{42: {sub}}}
	svc, err := New(store, Config{Subject: "mailto:admin@example.com", GeminiKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != sub.Endpoint {
			t.Errorf("unexpected endpoint %s", r.URL.Host)
		}
		if r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("missing encrypted content encoding")
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) == 0 || bytes.Contains(payload, []byte("SecretDevice")) || bytes.Contains(payload, []byte("SecretPlace")) {
			t.Error("payload is empty or unencrypted")
		}
		header := r.Header.Get("Authorization")
		tokenPart := strings.Split(strings.TrimPrefix(header, "vapid t="), ",")[0]
		pub, err := base64.RawURLEncoding.DecodeString(svc.PublicKey())
		if err != nil {
			t.Fatal(err)
		}
		px, py := elliptic.Unmarshal(elliptic.P256(), pub)
		token, err := jwt.Parse(tokenPart, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodES256 {
				t.Fatal("wrong JWT algorithm")
			}
			return &ecdsa.PublicKey{Curve: elliptic.P256(), X: px, Y: py}, nil
		}, jwt.WithAudience("https://fcm.googleapis.com"))
		if err != nil || !token.Valid {
			t.Fatalf("invalid VAPID JWT: %v", err)
		}
		if token.Claims.(jwt.MapClaims)["sub"] != "mailto:admin@example.com" {
			t.Errorf("incorrect VAPID subject: %v", token.Claims)
		}
		return &http.Response{StatusCode: http.StatusGone, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	event := sqlite.AlertEvent{UserID: 42, Kind: "entry", DeviceName: "SecretDevice", PlaceName: "SecretPlace"}
	if err := svc.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(store.deleted) != 1 || store.deleted[0] != "42:"+sub.Endpoint {
		t.Fatalf("delivery or cleanup failed: calls=%d deleted=%v", calls, store.deleted)
	}
	store.privacy = true
	if err := svc.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("sent in privacy mode")
	}
	store.privacy = false
	event.UserID = 999
	event.Humor = true
	if err := svc.Send(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("external call without subscriptions")
	}
}
