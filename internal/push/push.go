// Package push delivers encrypted Web Push notifications with VAPID.
package push

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/mleczakm/herring/internal/storage/sqlite"
)

type Store interface {
	PushSubscriptions(context.Context, int64) ([]sqlite.PushSubscription, error)
	DeletePushSubscription(context.Context, int64, string) error
	Settings(context.Context) (sqlite.Settings, error)
	EnsureVAPIDKeys(context.Context, string, string) (string, string, error)
}
type Config struct{ PublicKey, PrivateKey, Subject, GeminiKey, GeminiModel string }
type Service struct {
	store  Store
	config Config
	client *http.Client
}

func New(store Store, cfg Config) (*Service, error) {
	if cfg.PublicKey == "" && cfg.PrivateKey == "" {
		priv, pub, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			return nil, errors.New("generate VAPID keys failed")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cfg.PrivateKey, cfg.PublicKey, err = store.EnsureVAPIDKeys(ctx, priv, pub)
		if err != nil {
			return nil, errors.New("persist VAPID keys failed")
		}
	}
	if cfg.Subject == "" {
		cfg.Subject = "https://github.com/mleczakm/herring"
	}
	pub, e := base64.RawURLEncoding.DecodeString(cfg.PublicKey)
	if e != nil {
		return nil, errors.New("invalid VAPID public key")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), pub)
	if x == nil || y == nil {
		return nil, errors.New("invalid VAPID public key")
	}
	priv, e := base64.RawURLEncoding.DecodeString(cfg.PrivateKey)
	if e != nil || len(priv) != 32 {
		return nil, errors.New("invalid VAPID private key")
	}
	px, py := elliptic.P256().ScalarBaseMult(priv)
	if px.Cmp(x) != 0 || py.Cmp(y) != 0 {
		return nil, errors.New("VAPID key pair does not match")
	}
	if !strings.HasPrefix(cfg.Subject, "mailto:") && !strings.HasPrefix(cfg.Subject, "https://") {
		return nil, errors.New("VAPID subject must be mailto: or https://")
	}
	if cfg.GeminiModel == "" {
		cfg.GeminiModel = "gemini-3.5-flash-lite"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9._-]{1,80}$`).MatchString(cfg.GeminiModel) {
		return nil, errors.New("invalid Gemini model")
	}
	return &Service{store: store, config: cfg, client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *Service) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.config.PublicKey
}

// ValidateSubscription restricts endpoints to known browser push services and validates cryptographic keys.
func ValidateSubscription(sub sqlite.PushSubscription) error {
	u, e := url.Parse(sub.Endpoint)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" || len(sub.Endpoint) > 2048 {
		return errors.New("invalid push endpoint")
	}
	h := strings.ToLower(u.Hostname())
	allowed := h == "fcm.googleapis.com" || h == "updates.push.services.mozilla.com" || h == "push.services.mozilla.com" || h == "web.push.apple.com" || strings.HasSuffix(h, ".push.apple.com") || strings.HasSuffix(h, ".notify.windows.com")
	if !allowed {
		return errors.New("unsupported push service")
	}
	auth, e := base64.RawURLEncoding.DecodeString(sub.Auth)
	if e != nil || len(auth) != 16 {
		return errors.New("invalid push auth key")
	}
	p, e := base64.RawURLEncoding.DecodeString(sub.P256DH)
	if e != nil {
		return errors.New("invalid push public key")
	}
	x, _ := elliptic.Unmarshal(elliptic.P256(), p)
	if x == nil {
		return errors.New("invalid push public key")
	}
	return nil
}

// Send processes at most 100 subscriptions; callers should invoke it outside the ingest critical path.
func (s *Service) Send(ctx context.Context, event sqlite.AlertEvent) error {
	if s == nil {
		return nil
	}
	ctx, cancelAll := context.WithTimeout(ctx, 15*time.Second)
	defer cancelAll()
	subs, err := s.store.PushSubscriptions(ctx, event.UserID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return errors.New("push privacy check failed")
	}
	if settings.PrivacyMode && event.RevealsLocation() {
		return nil
	}
	body := localText(event)
	if event.Humor {
		body += " " + localHumor(event.Kind)
	}
	if s.config.GeminiKey != "" && event.Humor {
		if text := s.humor(ctx, event.Kind); text != "" {
			body = localText(event) + " " + text
		}
	}
	payload, _ := json.Marshal(map[string]string{"title": "Śledź", "body": body})
	var errs []error
	for _, sub := range subs {
		settings, settingsErr := s.store.Settings(ctx)
		if settingsErr != nil {
			return errors.New("push privacy check failed")
		}
		if settings.PrivacyMode && event.RevealsLocation() {
			return nil
		}
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}
		if err := ValidateSubscription(sub); err != nil {
			continue
		}
		reqctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		resp, err := webpush.SendNotificationWithContext(reqctx, payload, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.P256DH, Auth: sub.Auth}}, &webpush.Options{HTTPClient: s.client, Subscriber: strings.TrimPrefix(s.config.Subject, "mailto:"), VAPIDPublicKey: s.config.PublicKey, VAPIDPrivateKey: s.config.PrivateKey, TTL: 300})
		cancel()
		if err != nil {
			errs = append(errs, errors.New("push delivery failed"))
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			if e := s.store.DeletePushSubscription(ctx, event.UserID, sub.Endpoint); e != nil {
				errs = append(errs, e)
			}
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Errorf("push provider status %d", resp.StatusCode))
		}
	}
	return errors.Join(errs...)
}
func localText(e sqlite.AlertEvent) string {
	name := e.DeviceName
	if name == "" {
		name = "Lokalizator"
	}
	switch e.Kind {
	case "guard":
		return fmt.Sprintf("Wykryto ruch lub opuszczenie strefy ochrony przez pojazd „%s”. Sprawdź mapę.", name)
	case "service":
		return fmt.Sprintf("%s: czas na serwis „%s”.", name, e.PlaceName)
	case "monthly":
		return e.PlaceName
	case "entry":
		return fmt.Sprintf("%s jest już na miejscu „%s”.", name, e.PlaceName)
	case "exit":
		return fmt.Sprintf("%s jest już poza miejscem „%s”.", name, e.PlaceName)
	case "speed":
		return fmt.Sprintf("%s jedzie %.0f km/h i przekracza Twój limit.", name, e.SpeedKPH)
	case "offline":
		text := fmt.Sprintf("%s nie odzywa się od %s. Możliwe przyczyny: brak zasięgu, zasilania albo środków na karcie SIM.", name, silence(e.OfflineFor))
		if e.SMSRequested {
			text += " Wysłałem SMS z prośbą o pozycję."
		}
		return text
	case "online":
		return fmt.Sprintf("%s znów jest w kontakcie.", name)
	case "sms_position":
		return fmt.Sprintf("Przyszła pozycja SMS-em od: %s. Sprawdź mapę.", name)
	default:
		return "Nowy alert lokalizatora."
	}
}

func localHumor(kind string) string {
	switch kind {
	case "entry":
		return "Śledź melduje: cel na horyzoncie!"
	case "exit":
		return "Szykuj się, ktoś już jest w drodze!"
	case "speed":
		return "Pośpiech poniża!"
	case "offline":
		return "Śledź zarzuca sieci!"
	case "online":
		return "Śledź znów na tropie!"
	case "sms_position":
		return "Gołąb pocztowy doleciał!"
	}
	return "Śledź czuwa!"
}

// Only a fixed category, never user-provided names, positions, or history, reaches Gemini.
func (s *Service) humor(ctx context.Context, kind string) string {
	var subject string
	switch kind {
	case "entry":
		subject = "przybyciu"
	case "exit":
		subject = "wyjeździe"
	case "speed":
		subject = "prędkości"
	default:
		return ""
	}
	prompt := "Napisz jedno krótkie, łagodne i zabawne zdanie po polsku o " + subject + ". Bez imion, miejsc, liczb i cudzysłowów."
	data, _ := json.Marshal(map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]string{"text": prompt}}}}, "generationConfig": map[string]any{"maxOutputTokens": 80}})
	reqctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqctx, "POST", "https://generativelanguage.googleapis.com/v1beta/models/"+s.config.GeminiModel+":generateContent", bytes.NewReader(data))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", s.config.GeminiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&out) != nil || len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return ""
	}
	text := strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text)
	if !utf8.ValidString(text) || len([]rune(text)) > 180 || strings.ContainsAny(text, "\r\n") {
		return ""
	}
	return text
}

// silence formats an outage length after "od", which takes the genitive in
// Polish: "od 45 minut", "od 1 godziny", "od 5 godzin", "od 3 dni".
func silence(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d minut", int(d.Minutes()))
	case d < 2*time.Hour:
		return "1 godziny"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d godzin", int(d.Hours()))
	default:
		return fmt.Sprintf("%d dni", int(d.Hours()/24))
	}
}
