package push

import (
	"strings"
	"testing"
	"time"

	"github.com/mleczakm/herring/internal/storage/sqlite"
)

func TestOfflineTextsRevealNoLocation(t *testing.T) {
	cases := []struct {
		event sqlite.AlertEvent
		want  string
	}{
		{sqlite.AlertEvent{Kind: "offline", DeviceName: "Auto", OfflineFor: 45 * time.Minute}, "Auto nie odzywa się od 45 minut. Możliwe przyczyny: brak zasięgu, zasilania albo środków na karcie SIM."},
		{sqlite.AlertEvent{Kind: "offline", DeviceName: "Auto", OfflineFor: 5 * time.Hour, SMSRequested: true}, "Auto nie odzywa się od 5 godzin. Możliwe przyczyny: brak zasięgu, zasilania albo środków na karcie SIM. Wysłałem SMS z prośbą o pozycję."},
		{sqlite.AlertEvent{Kind: "offline", DeviceName: "Auto", OfflineFor: 72 * time.Hour}, "Auto nie odzywa się od 3 dni."},
		{sqlite.AlertEvent{Kind: "offline", DeviceName: "Auto", OfflineFor: 90 * time.Minute}, "Auto nie odzywa się od 1 godziny."},
		{sqlite.AlertEvent{Kind: "online", DeviceName: "Auto"}, "Auto znów jest w kontakcie."},
		{sqlite.AlertEvent{Kind: "sms_position", DeviceName: "Auto"}, "Przyszła pozycja SMS-em od: Auto. Sprawdź mapę."},
	}
	for _, c := range cases {
		if got := localText(c.event); !strings.HasPrefix(got, c.want) {
			t.Errorf("localText(%s) = %q, want prefix %q", c.event.Kind, got, c.want)
		}
		if c.event.RevealsLocation() {
			t.Errorf("%s is treated as revealing location", c.event.Kind)
		}
	}
	for _, kind := range []string{"entry", "exit", "speed"} {
		if !(sqlite.AlertEvent{Kind: kind}).RevealsLocation() {
			t.Errorf("%s must be held back in privacy mode", kind)
		}
	}
}
