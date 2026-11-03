package plcservice

import (
	"testing"

	"github.com/mochigome-git/msp-go/pkg/config"
)

func TestBuildWriteMap_MultipleTargetsPerSource(t *testing.T) {
	cfg := config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D71>D,3026,1,1;D72>D,3026,1,1"},
		{Name: "secondary", WriteMap: "D3026>D,1,1,1;D3026>D,2,1,1"},
	}}

	wm := BuildWriteMap(cfg)

	targets := wm.Default["D3026"]
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets for D3026, got %d", len(targets))
	}
	for i, want := range []string{"1", "2"} {
		if targets[i].PLCName != "secondary" || targets[i].Device.DeviceType != "D" || targets[i].Device.DeviceNumber != want {
			t.Errorf("target %d = %+v, want secondary D%s", i, targets[i], want)
		}
	}
	if got := wm.Default["D71"]; len(got) != 1 || got[0].PLCName != "main" {
		t.Errorf("D71 targets = %+v, want one main target", got)
	}
}

func TestParseCondRules_StripsQuotes(t *testing.T) {
	rules := ParseCondRules(`"M64==D71;M80==D71"`)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Bit != "M64" || rules[1].Src != "D71" {
		t.Errorf("unexpected rules: %+v", rules)
	}
}
