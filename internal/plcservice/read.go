package plcservice

import (
	"context"
	"fmt"
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
func (s *Service) ReadAndEnqueue(ctx context.Context, wp WorkerPool) {
	for plcName, devList := range s.devices {
		s.mu.Lock()
		plan := s.plans[plcName]
		s.mu.Unlock()

		if plan != nil {
			s.readPlanned(ctx, wp, plcName)
			continue
		}
		for _, device := range devList {
			s.readOne(ctx, wp, plcName, device)
		}
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
			s.logger.Printf("[%s] block %s%d x%d failed: %v — falling back to single reads",
				plcName, b.DeviceType, b.Start, b.Words, err)
			for _, m := range b.Members {
				s.readOne(ctx, wp, plcName, m.Device)
			}
			continue
		}
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
		s.readOne(ctx, wp, plcName, d)
	}
}

// readOne is the original per-device read + enqueue.
func (s *Service) readOne(ctx context.Context, wp WorkerPool, plcName string, device PLC_Utils.Device) {
	devCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	val, err := s.ReadDevice(devCtx, plcName, device)
	cancel()

	if err != nil {
		if err == context.DeadlineExceeded {
			s.logger.Printf("[%s] Timeout reading %s, skipping", plcName, device.DeviceType+device.DeviceNumber)
			return
		}
		s.logger.Printf("[%s] Failed reading %s: %v", plcName, device.DeviceType+device.DeviceNumber, err)
		return
	}

	wp.Enqueue(map[string]any{
		"address": device.DeviceType + device.DeviceNumber,
		"value":   val,
		"source":  plcName,
	})
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
