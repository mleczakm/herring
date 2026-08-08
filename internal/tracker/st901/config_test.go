package st901

import "testing"

func TestCommands(t *testing.T) {
	p := Profile{ControlNumber: "+48 500-600-700", APN: "internet", ServerHost: "65.108.44.244", ServerPort: 20115, MovingInterval: 20, StoppedInterval: 300}
	commands, err := p.Commands()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"485006007000000 1", "8030000 internet", "8040000 65.108.44.244 20115", "8050000 20", "8090000 300", "7100000", "RCONF"}
	for i := range want {
		if commands[i].Body != want[i] {
			t.Errorf("command %d = %q, want %q", i, commands[i].Body, want[i])
		}
	}
}

func TestConfigurationMatches(t *testing.T) {
	p := Profile{APN: "internet", ServerHost: "65.108.44.244", ServerPort: 20115}
	if !ConfigurationMatches("ID:123; MODE:GPRS; APN:internet; IP:65.108.44.244:20115", p) {
		t.Fatal("matching RCONF rejected")
	}
	if ConfigurationMatches("MODE:GPRS; APN:internet; IP:1.2.3.4:20115", p) {
		t.Fatal("wrong endpoint accepted")
	}
}
