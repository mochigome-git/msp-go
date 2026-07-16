package plcservice

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mochigome-git/msp-go/pkg/config"
	// CHANGED: was pkg/plc — EncodeData now lives in pkg/plc/mitsubishi
	// If you later need Shibaura encoding, add a shibaura.EncodeData too.

	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// WriteTarget, SimpleCondWrite, WriteMapWithCond — all unchanged from original.

type WriteTarget struct {
	PLCName string
	Device  PLC_Utils.Device
}

type SimpleCondWrite struct {
	Bit      string
	Operator string
	Src      string
}

type WriteMapWithCond struct {
	Default map[string]WriteTarget
	Cond    []SimpleCondWrite
}

// WriteDevice — identical to original except EncodeData call uses mitsubishi package.
func (s *Service) WriteDevice(ctx context.Context, plcName string, device PLC_Utils.Device, value any) error {
	s.mu.Lock()
	client, ok := s.clients[plcName]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("PLC client %s not found", plcName)
	}

	writeOne := func(valStr string, processNumber int) error {
		data, err := client.EncodeData(valStr, processNumber)
		if err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() {
			// CHANGED: was client.Write(device, data)
			// Now call WriteData or BatchWrite directly on PLCClient
			if device.NumberRegisters == 1 {
				done <- client.WriteData(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters)
			} else {
				done <- client.BatchWrite(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters, log.Default())
			}
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

// BuildWriteMap — identical to original, no changes needed.
func BuildWriteMap(cfg config.AppConfig) WriteMapWithCond {
	defaultMap := make(map[string]WriteTarget)
	var condRules []SimpleCondWrite

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
				src, dest, ok := strings.Cut(pair, ">")
				if !ok {
					continue
				}
				src = strings.TrimSpace(src)
				dest = strings.TrimSpace(dest)

				destParts := strings.Split(dest, ",")
				if len(destParts) < 4 {
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

				defaultMap[src] = WriteTarget{
					PLCName: plcCfg.Name,
					Device:  device,
				}
			}
		}
		condRules = append(condRules, ParseCondRules(plcCfg.CondMap)...)
	}

	return WriteMapWithCond{Default: defaultMap, Cond: condRules}
}

// DirectWrite — identical to original, no changes needed.
func (s *Service) DirectWrite(ctx context.Context, msg map[string]any, writeMap WriteMapWithCond) error {
	addrVal, exists := msg["address"]
	if !exists {
		return fmt.Errorf("missing address in message")
	}
	addr, ok := addrVal.(string)
	if !ok {
		return fmt.Errorf("address must be a string")
	}
	value, exists := msg["value"]
	if !exists {
		return fmt.Errorf("missing value in message")
	}

	written := make(map[string]bool)
	conditionalDevices := make(map[string]bool)

	for _, rule := range writeMap.Cond {
		conditionalDevices[rule.Src] = true
		conditionalDevices[rule.Bit] = true
	}
	if conditionalDevices[addr] {
		s.StoreDeviceValue(addr, value)
	}

	for _, rule := range writeMap.Cond {
		bitVal, exists := s.GetDeviceValue(rule.Bit)
		if !exists {
			continue
		}
		bitInt, ok := toInt(bitVal)
		if !ok {
			s.logger.Printf("Warning: cannot convert value to int for bit device %s: %v", rule.Bit, bitVal)
			continue
		}
		match := false
		switch rule.Operator {
		case "==":
			match = bitInt == 1
		case "!=":
			match = bitInt != 1
		}
		if match {
			if target, ok := writeMap.Default[rule.Src]; ok {
				srcVal, exists := s.GetDeviceValue(rule.Src)
				if !exists {
					continue
				}
				if err := s.WriteDevice(ctx, target.PLCName, target.Device, srcVal); err != nil {
					return err
				}
				written[rule.Src] = true
				s.ClearDeviceValue(rule.Src)
			}
		}
	}

	if !conditionalDevices[addr] && !written[addr] {
		if target, ok := writeMap.Default[addr]; ok {
			intVal, ok := toInt(value)
			if !ok {
				return nil
			}
			if intVal != 0 {
				return s.WriteDevice(ctx, target.PLCName, target.Device, value)
			}
		}
	}

	return nil
}
