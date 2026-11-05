package plcservice

import (
	"context"
	"log"
	"reflect"
	"sync"
	"testing"

	"github.com/mochigome-git/msp-go/pkg/config"
)

// The MCS_SYNC config: D71/D72 (secondary) → D3026 (main), guarded by
// "M64==D71;M80==D71". D72 has no rule, so it must never reach main.
func mcsSyncMap() WriteMapWithCond {
	return BuildWriteMap(config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "main", WriteMap: "D71>D,3026,1,1;D72>D,3026,1,1", CondMap: `"M64==D71;M80==D71"`},
		{Name: "secondary", WriteMap: "D3026>D,1,1,1;D3026>D,2,1,1;X3E>M,70,3,1"},
	}})
}

func TestDirectWrite_ConditionGatesSecToPrim(t *testing.T) {
	main, sec := &recPLC{}, &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = main, sec
	wm := mcsSyncMap()
	ctx := context.Background()

	step := func(addr string, v any) {
		t.Helper()
		if err := s.DirectWrite(ctx, map[string]any{"address": addr, "value": v}, wm); err != nil {
			t.Fatal(err)
		}
	}

	// Machine idle: M64=0, M80=0. Nothing may reach main.
	step("M64", uint8(0))
	step("M80", uint8(0))
	step("D71", uint16(7))
	step("D72", uint16(9))
	if len(main.writes) != 0 {
		t.Fatalf("condition off but main got writes: %v", main.writes)
	}

	// M64 → 1 releases D71 only. D72 has no rule and stays blocked.
	step("M64", uint8(1))
	step("D72", uint16(9))
	if want := []string{"D3026=7"}; !reflect.DeepEqual(main.writes, want) {
		t.Fatalf("main writes = %v, want %v", main.writes, want)
	}

	// PRIM_TO_SEC has no condition and still syncs to both targets.
	step("D3026", uint16(5))
	if want := []string{"D1=5", "D2=5"}; !reflect.DeepEqual(sec.writes, want) {
		t.Fatalf("secondary writes = %v, want %v", sec.writes, want)
	}
}

// 15 workers racing on the same stored value must write it once.
func TestDirectWrite_ConcurrentConditionWritesOnce(t *testing.T) {
	main := &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["main"], s.clients["secondary"] = main, &recPLC{}
	wm := mcsSyncMap()
	ctx := context.Background()

	_ = s.DirectWrite(ctx, map[string]any{"address": "D71", "value": uint16(7)}, wm)
	s.StoreDeviceValue("M64", uint8(1))

	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.DirectWrite(ctx, map[string]any{"address": "M80", "value": uint8(0)}, wm)
		}()
	}
	wg.Wait()
	if len(main.writes) != 1 {
		t.Fatalf("expected 1 write, got %v", main.writes)
	}
}
