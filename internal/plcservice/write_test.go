package plcservice

import (
	"context"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mochigome-git/msp-go/pkg/config"
)

func TestParseWriteMap_MultipleTargetsPerSource(t *testing.T) {
	cfg := config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D71>D,3026,1,1;D72>D,3026,1,1"},
		{Name: "secondary", WriteMap: "D3026>D,1,1,1;D3026>D,2,1,1"},
	}}

	wm := mustWriteMap(t, cfg)

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
	rules, err := ParseCondRules(`"M64==D71;M80==D71"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Bit != "M64" || rules[1].Src != "D71" {
		t.Errorf("unexpected rules: %+v", rules)
	}
}

// The formats the old .env example documented must be rejected, not
// silently dropped.
func TestParseWriteMap_RejectsMalformedEntries(t *testing.T) {
	for _, tc := range []struct{ writeMap, cond string }{
		{writeMap: "D,100|D,200"},                 // old documented WRITE_MAP format
		{writeMap: "D100>D"},                      // missing number
		{writeMap: "D100>D,200,x,1"},              // bad write type
		{writeMap: "D100>D,200,1,1,9"},            // too many fields
		{writeMap: "D100>D,200", cond: "M,64==1"}, // old documented condition format
		{writeMap: "D100>D,200", cond: "M64>=D100"},
	} {
		_, err := ParseWriteMap(config.AppConfig{PLCs: []config.PLCConfig{
			{Name: "main", WriteMap: tc.writeMap, CondMap: tc.cond},
		}})
		if err == nil {
			t.Errorf("WriteMap=%q Cond=%q: expected error", tc.writeMap, tc.cond)
		}
	}
}

func TestParseWriteMap_Defaults(t *testing.T) {
	wm := mustWriteMap(t, config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D100>D,200;X3E>M,70,3"},
	}})
	if d := wm.Default["D100"][0].Device; d.ProcessNumber != 1 || d.NumberRegisters != 1 {
		t.Errorf("D100 target = %+v, want type 1, 1 register", d)
	}
	if d := wm.Default["X3E"][0].Device; d.ProcessNumber != 3 || d.NumberRegisters != 1 {
		t.Errorf("X3E target = %+v, want type 3, 1 register", d)
	}
}

func TestValidateWriteMap(t *testing.T) {
	cfg := config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D100>D,200,1,1"},
		{Name: "secondary", Brand: "shibaura", WriteMap: "D200>D,100,1,1"},
	}}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = &recPLC{}, &recPLC{}
	err := s.ValidateWriteMap(cfg, mustWriteMap(t, cfg))
	if err == nil || !strings.Contains(err.Error(), "shibaura") {
		t.Fatalf("want error for write to shibaura, got %v", err)
	}

	cfg.PLCs[0].WriteMap = "D100>R,200,1,1" // R is not supported by the 3E builder
	cfg.PLCs[1].WriteMap = ""
	if err := s.ValidateWriteMap(cfg, mustWriteMap(t, cfg)); err == nil {
		t.Fatal("want error for unsupported device type")
	}

	delete(s.clients, "main")
	cfg.PLCs[0].WriteMap = "D100>D,200,1,1"
	if err := s.ValidateWriteMap(cfg, mustWriteMap(t, cfg)); err == nil {
		t.Fatal("want error for target PLC that is not configured")
	}
}

// A condition bit from a PLC that went down must not keep releasing writes.
func TestDirectWrite_ConditionClearedWhenPLCDown(t *testing.T) {
	main := &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = main, &recPLC{}
	wm := mcsSyncMap(t)
	ctx := context.Background()

	_ = s.DirectWrite(ctx, map[string]any{"address": "M64", "value": uint8(1), "source": "main"}, wm)
	s.markDown("main", errors.New("connection reset"))
	_ = s.DirectWrite(ctx, map[string]any{"address": "D71", "value": uint16(7), "source": "secondary"}, wm)
	if len(main.writes) != 0 {
		t.Fatalf("stale M64 released a write: %v", main.writes)
	}
}

func TestDirectWrite_ConditionIgnoredWhenStale(t *testing.T) {
	main := &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = main, &recPLC{}
	s.valueMaxAge = 20 * time.Millisecond
	wm := mcsSyncMap(t)
	ctx := context.Background()

	_ = s.DirectWrite(ctx, map[string]any{"address": "M64", "value": uint8(1), "source": "main"}, wm)
	time.Sleep(40 * time.Millisecond)
	_ = s.DirectWrite(ctx, map[string]any{"address": "D71", "value": uint16(7), "source": "secondary"}, wm)
	if len(main.writes) != 0 {
		t.Fatalf("stale M64 released a write: %v", main.writes)
	}
}

// failPLC fails every write.
type failPLC struct{ recPLC }

func (p *failPLC) WriteData(string, string, []byte, uint16) error { return errors.New("boom") }

func TestDirectWrite_FailedTargetDoesNotStopOthers(t *testing.T) {
	good := &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = &failPLC{}, good
	wm := mustWriteMap(t, config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D1>D,10"},
		{Name: "secondary", WriteMap: "D1>D,20"},
	}})
	err := s.DirectWrite(context.Background(), map[string]any{"address": "D1", "value": uint16(3)}, wm)
	if err == nil {
		t.Fatal("expected error from failing target")
	}
	if want := []string{"D20=3"}; !reflect.DeepEqual(good.writes, want) {
		t.Fatalf("secondary writes = %v, want %v", good.writes, want)
	}
}
