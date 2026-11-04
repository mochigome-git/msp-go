package plcservice

import (
	"context"
	"log"
	"strconv"
	"sync"
	"testing"

	"github.com/mochigome-git/msp-go/pkg/config"
)

// recPLC records every write it receives.
type recPLC struct {
	mu     sync.Mutex
	writes []string // "M70=0x01"
}

func (p *recPLC) ReadData(context.Context, string, string, uint16, bool) (any, error) {
	return nil, nil
}
func (p *recPLC) WriteData(dt, dn string, data []byte, _ uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, dt+dn+"="+strconv.Itoa(int(data[0])))
	return nil
}
func (p *recPLC) BatchWrite(string, string, []byte, uint16, *log.Logger) error { return nil }
func (p *recPLC) EncodeData(v string, proc int) ([]byte, error) {
	if proc == 3 {
		if v == "1" {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	}
	n, _ := strconv.Atoi(v)
	return []byte{byte(n), byte(n >> 8)}, nil
}

// X3E>M70 must follow X3E both ON and OFF; D3026>D1 keeps the old
// "zero is not synced" behaviour.
func TestDirectWrite_BitTargetSyncsZero(t *testing.T) {
	sec := &recPLC{}
	s := NewService(log.New(&discard{}, "", 0))
	s.clients["secondary"] = sec

	wm := BuildWriteMap(config.AppConfig{PLCs: []config.PLCConfig{
		{Name: "secondary", WriteMap: "D3026>D,1,1,1;X3E>M,70,3,1"},
	}})
	ctx := context.Background()

	for _, m := range []map[string]any{
		{"address": "X3E", "value": uint8(1)},
		{"address": "X3E", "value": uint8(0)},
		{"address": "D3026", "value": uint16(0)},
		{"address": "D3026", "value": uint16(5)},
	} {
		if err := s.DirectWrite(ctx, m, wm); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"M70=1", "M70=0", "D1=5"}
	if len(sec.writes) != len(want) {
		t.Fatalf("writes = %v, want %v", sec.writes, want)
	}
	for i := range want {
		if sec.writes[i] != want[i] {
			t.Errorf("write %d = %s, want %s", i, sec.writes[i], want[i])
		}
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
