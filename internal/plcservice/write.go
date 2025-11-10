package plcservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/plc"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// WriteTarget links a source address to a PLC device
type WriteTarget struct {
	PLCName string
	Device  PLC_Utils.Device
}

// WriteDevice writes a value to a single device on a specific PLC with type safety
func (s *Service) WriteDevice(ctx context.Context, plcName string, device PLC_Utils.Device, value any) error {
	s.mu.Lock()
	client, ok := s.clients[plcName]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("PLC client %s not found", plcName)
	}

	writeOne := func(valStr string, processNumber int) error {
		data, err := plc.EncodeData(valStr, processNumber)
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() {
			done <- client.Write(device, data)
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return err
		}
	}

	switch v := value.(type) {
	case string:
		return writeOne(v, int(device.ProcessNumber))
	case []string:
		for _, val := range v {
			if err := writeOne(val, int(device.ProcessNumber)); err != nil {
				return err
			}
		}
	case bool:
		return writeOne(fmt.Sprintf("%t", v), int(device.ProcessNumber))
	case int, int8, int16, int32, int64:
		return writeOne(fmt.Sprintf("%d", v), int(device.ProcessNumber))
	case uint, uint8, uint16, uint32, uint64:
		return writeOne(fmt.Sprintf("%d", v), int(device.ProcessNumber))
	case float32, float64:
		return writeOne(fmt.Sprintf("%f", v), int(device.ProcessNumber))
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return nil
}

// BuildWriteMap parses config to map source → WriteTarget
func BuildWriteMap(cfg config.AppConfig) map[string]WriteTarget {
	result := make(map[string]WriteTarget)

	for _, plcCfg := range cfg.PLCs {
		mapStr := plcCfg.WriteMap
		start := 0

		for i := 0; i <= len(mapStr); i++ {
			if i == len(mapStr) || mapStr[i] == ';' {
				pair := strings.TrimSpace(mapStr[start:i])
				start = i + 1
				if pair == "" {
					continue
				}

				// Split source > destination
				src, dest, ok := strings.Cut(pair, ">")
				if !ok {
					continue
				}
				src = strings.TrimSpace(src)
				dest = strings.TrimSpace(dest)

				// Destination format: D3302,1,1,1  -> DeviceType, DeviceNumber, ProcessNumber, NumberRegisters
				destParts := strings.Split(dest, ",")
				if len(destParts) < 4 {
					// fallback to 1 register and process 1
					destParts = append(destParts, "1", "1")
				}

				deviceType := strings.TrimSpace(destParts[0])
				deviceNumber := strings.TrimSpace(destParts[1])

				numRegs := 1
				if n, err := strconv.Atoi(strings.TrimSpace(destParts[3])); err == nil {
					numRegs = n
				}

				processNumber := 1
				if n, err := strconv.Atoi(strings.TrimSpace(destParts[2])); err == nil {
					processNumber = n
				}

				device := PLC_Utils.Device{
					DeviceType:      deviceType,
					DeviceNumber:    deviceNumber,
					NumberRegisters: uint16(numRegs),
					ProcessNumber:   uint16(processNumber),
				}

				result[src] = WriteTarget{
					PLCName: plcCfg.Name,
					Device:  device,
				}
			}
		}
	}

	return result
}

// DirectWrite writes a message according to the write map
func (s *Service) DirectWrite(ctx context.Context, msg map[string]any, writeMap map[string]WriteTarget) error {
	addr, ok := msg["address"].(string)
	if !ok {
		return fmt.Errorf("missing address in message")
	}

	target, ok := writeMap[addr]
	if !ok {
		return nil //fmt.Errorf("address %s not in write map", addr)
	}

	// Use the PLCName from WriteTarget
	return s.WriteDevice(ctx, target.PLCName, target.Device, msg["value"])
}
