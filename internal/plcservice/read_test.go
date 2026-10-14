package plcservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// stubPLC fails every read with err while down, otherwise returns 1.
type stubPLC struct {
	mu    sync.Mutex
	down  bool
	err   error
	reads int
}

func (p *stubPLC) ReadData(ctx context.Context, dt, dn string, n uint16, fx bool) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.down {
		return nil, p.err
	}
	return uint16(1), nil
}
func (p *stubPLC) WriteData(string, string, []byte, uint16) error { return nil }
func (p *stubPLC) BatchWrite(string, string, []byte, uint16, *log.Logger) error {
	return nil
}
func (p *stubPLC) EncodeData(string, int) ([]byte, error) { return nil, nil }

type countPool struct{ n int }

func (c *countPool) Enqueue(map[string]any) { c.n++ }

func newTestService(p *stubPLC, devices int) (*Service, *bytes.Buffer) {
	var buf bytes.Buffer
	s := NewService(log.New(&buf, "", 0))
	s.clients["main"] = p
	for i := 0; i < devices; i++ {
		s.devices["main"] = append(s.devices["main"],
			PLC_Utils.Device{DeviceType: "D", DeviceNumber: fmt.Sprint(i), NumberRegisters: 1})
	}
	return s, &buf
}

// The dial error msp-go produces when a PLC doesn't answer.
func dialTimeoutErr() error {
	_, err := net.DialTimeout("tcp", "10.255.255.1:5011", time.Millisecond)
	return fmt.Errorf("connect PLC 10.255.255.1:5011: %w", err)
}

func TestReadAndEnqueue_UnreachablePLCFailsFast(t *testing.T) {
	err := dialTimeoutErr()
	if !isConnError(err) {
		t.Fatalf("dial error not classified as connection error: %v", err)
	}

	p := &stubPLC{down: true, err: err}
	s, logs := newTestService(p, 140)
	wp := &countPool{}

	s.ReadAndEnqueue(context.Background(), wp)
	if p.reads != 1 {
		t.Fatalf("unreachable PLC: %d read attempts in one cycle, want 1", p.reads)
	}

	// During backoff: no attempts, and the loop doesn't spin.
	start := time.Now()
	s.ReadAndEnqueue(context.Background(), wp)
	if p.reads != 1 {
		t.Fatalf("read attempted during backoff")
	}
	if time.Since(start) < 400*time.Millisecond {
		t.Fatalf("ReadAndEnqueue returned immediately while all PLCs back off (tight loop)")
	}

	// Recovery: after backoff, a full cycle runs and "reachable again" is logged once.
	p.mu.Lock()
	p.down = false
	p.mu.Unlock()
	s.mu.Lock()
	s.downUntil["main"] = time.Time{}
	s.mu.Unlock()
	s.ReadAndEnqueue(context.Background(), wp)
	if wp.n != 140 {
		t.Fatalf("after recovery enqueued %d, want 140", wp.n)
	}
	if c := strings.Count(logs.String(), "PLC unreachable"); c != 1 {
		t.Fatalf("logged 'unreachable' %d times, want 1:\n%s", c, logs)
	}
	if c := strings.Count(logs.String(), "reachable again"); c != 1 {
		t.Fatalf("logged 'reachable again' %d times, want 1", c)
	}
}

// A per-device error (e.g. PLC end code) must not stop the other devices.
func TestReadAndEnqueue_DeviceErrorDoesNotSkipPLC(t *testing.T) {
	p := &stubPLC{down: true, err: errors.New("PLC error end code C056")}
	s, _ := newTestService(p, 10)
	s.ReadAndEnqueue(context.Background(), &countPool{})
	if p.reads != 10 {
		t.Fatalf("device-level errors: %d reads, want 10", p.reads)
	}
}
