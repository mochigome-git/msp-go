package plcservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// WorkerPool interface for enqueue
type WorkerPool interface {
	Enqueue(msg map[string]any)
}

// ReadAndEnqueue reads all devices from all PLCs and enqueues to worker pool.
// Message shape and address strings are unchanged whichever read path is
// used, so downstream consumers (gopub-edge) need no changes. Only the
// order of messages within a cycle differs in block mode (grouped by block).
//
// If a PLC can't be reached (connect/read timeout, connection reset), the
// rest of its devices are skipped for this cycle and the PLC is retried
// after unreachableBackoff. Without this, an unplugged PLC costs one I/O
// timeout per device — 140 devices × 5s ≈ 12 minutes per cycle.
func (s *Service) ReadAndEnqueue(ctx context.Context, wp WorkerPool) {
	readAny := false
	for plcName, devList := range s.devices {
		if len(devList) == 0 || s.backingOff(plcName) {
			continue
		}
		readAny = true

		s.mu.Lock()
		plan := s.plans[plcName]
		s.mu.Unlock()

		if plan != nil {
			s.readPlanned(ctx, wp, plcName)
			continue
		}
		for _, device := range devList {
			if s.readOne(ctx, wp, plcName, device) {
				break // PLC unreachable — skip the rest this cycle
			}
		}
	}

	// The app calls ReadAndEnqueue in a tight loop; don't spin while every
	// PLC is in backoff.
	if !readAny {
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
}

const unreachableBackoff = 5 * time.Second

// isConnError reports whether err means the PLC itself is unreachable
// (as opposed to a per-device problem such as a PLC error end code).
func isConnError(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func (s *Service) backingOff(plcName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.downUntil[plcName])
}

// markDown records a connection failure; logs only on the first failure
// so a dead PLC produces one line per retry, not one per device.
func (s *Service) markDown(plcName string, err error) {
	s.mu.Lock()
	wasDown := s.down[plcName]
	s.down[plcName] = true
	s.downUntil[plcName] = time.Now().Add(unreachableBackoff)
	s.mu.Unlock()
	if !wasDown {
		s.logger.Printf("[%s] PLC unreachable: %v — skipping its reads, retrying every %s",
			plcName, err, unreachableBackoff)
	}
}

func (s *Service) markUp(plcName string) {
	s.mu.Lock()
	wasDown := s.down[plcName]
	s.down[plcName] = false
	s.mu.Unlock()
	if wasDown {
		s.logger.Printf("[%s] PLC reachable again", plcName)
	}
}

// readPlanned executes the block plan for one PLC. If a block fails, its
// devices fall back to single reads for this cycle so one bad block never
// loses data that the old path would have delivered.
func (s *Service) readPlanned(ctx context.Context, wp WorkerPool, plcName string) {
	s.mu.Lock()
	client := s.clients[plcName]
	plan := s.plans[plcName]
	s.mu.Unlock()

	br, ok := client.(blockReader)
	if !ok {
		return
	}

	for _, b := range plan.Blocks {
		bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		values, err := br.ReadBlock(bctx, b)
		cancel()

		if err != nil {
			if isConnError(err) {
				s.markDown(plcName, err)
				return
			}
			s.logger.Printf("[%s] block %s%d x%d failed: %v — falling back to single reads",
				plcName, b.DeviceType, b.Start, b.Words, err)
			for _, m := range b.Members {
				if s.readOne(ctx, wp, plcName, m.Device) {
					return
				}
			}
			continue
		}
		s.markUp(plcName)
		for _, v := range values {
			addr := v.Device.DeviceType + v.Device.DeviceNumber
			if v.Err != nil {
				s.logger.Printf("[%s] Failed decoding %s: %v", plcName, addr, v.Err)
				continue
			}
			wp.Enqueue(map[string]any{"address": addr, "value": v.Value, "source": plcName})
		}
	}

	for _, d := range plan.Singles {
		if s.readOne(ctx, wp, plcName, d) {
			return
		}
	}
}

// readOne is the original per-device read + enqueue. It returns true if
// the PLC is unreachable, so the caller can stop reading that PLC.
func (s *Service) readOne(ctx context.Context, wp WorkerPool, plcName string, device PLC_Utils.Device) (unreachable bool) {
	devCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	val, err := s.ReadDevice(devCtx, plcName, device)
	cancel()

	if err != nil {
		if isConnError(err) {
			s.markDown(plcName, err)
			return true
		}
		if err == context.DeadlineExceeded {
			s.logger.Printf("[%s] Timeout reading %s, skipping", plcName, device.DeviceType+device.DeviceNumber)
			return false
		}
		if ctx.Err() == nil {
			s.logger.Printf("[%s] Failed reading %s: %v", plcName, device.DeviceType+device.DeviceNumber, err)
		}
		return false
	}
	s.markUp(plcName)

	wp.Enqueue(map[string]any{
		"address": device.DeviceType + device.DeviceNumber,
		"value":   val,
		"source":  plcName,
	})
	return false
}

// ReadDevice reads a single device from a specific PLC
func (s *Service) ReadDevice(ctx context.Context, plcName string, device PLC_Utils.Device) (any, error) {
	s.mu.Lock()
	client, ok := s.clients[plcName]
	fx := s.fx[plcName] // ← look up by PLC name
	s.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("PLC client %s not found", plcName)
	}

	return client.ReadData(ctx, device.DeviceType, device.DeviceNumber, device.NumberRegisters, fx)
}
