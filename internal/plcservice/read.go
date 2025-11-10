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

// ReadAndEnqueue reads all devices from all PLCs and enqueues to worker pool
func (s *Service) ReadAndEnqueue(ctx context.Context, wp WorkerPool) {
	for plcName, devList := range s.devices {
		for _, device := range devList {
			devCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			val, err := s.ReadDevice(devCtx, plcName, device)
			cancel()

			if err != nil {
				if err == context.DeadlineExceeded {
					s.logger.Printf("[%s] Timeout reading %s, skipping", plcName, device.DeviceType+device.DeviceNumber)
					continue
				}
				s.logger.Printf("[%s] Failed reading %s: %v", plcName, device.DeviceType+device.DeviceNumber, err)
				continue
			}

			msg := map[string]any{
				"address": device.DeviceType + device.DeviceNumber,
				"value":   val,
				"source":  plcName,
			}
			wp.Enqueue(msg)
		}
	}
}

// ReadDevice reads a single device from a specific PLC
func (s *Service) ReadDevice(ctx context.Context, plcName string, device PLC_Utils.Device) (any, error) {
	s.mu.Lock()
	client, ok := s.clients[plcName]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("PLC client %s not found", plcName)
	}
	return client.Read(ctx, device, s.fx)
}
