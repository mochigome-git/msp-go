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

type SimpleCondWrite struct {
	Bit      string // e.g. "M64"
	Operator string // "==" or "!="
	Src      string // target source to write if condition matches
}

type WriteMapWithCond struct {
	Default map[string]WriteTarget
	Cond    []SimpleCondWrite
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
func BuildWriteMap(cfg config.AppConfig) WriteMapWithCond {
	defaultMap := make(map[string]WriteTarget)
	var condRules []SimpleCondWrite

	for _, plcCfg := range cfg.PLCs {
		mapStr := plcCfg.WriteMap
		start := 0

		// Build default mapping
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

		// Parse conditional rules
		condRules = append(condRules, ParseCondRules(plcCfg.CondMap)...)
	}

	return WriteMapWithCond{
		Default: defaultMap,
		Cond:    condRules,
	}
}

// DirectWrite writes a message according to the write map
// DirectWrite writes a message according to the write map with conditional logic
func (s *Service) DirectWrite(ctx context.Context, msg map[string]any, writeMap WriteMapWithCond) error {
	// Extract device address and value from message
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

	// Build set of conditional devices
	for _, rule := range writeMap.Cond {
		conditionalDevices[rule.Src] = true
		conditionalDevices[rule.Bit] = true
	}
	// Store the current value for future conditional checks
	if conditionalDevices[addr] {
		s.StoreDeviceValue(addr, value)
	}
	//	s.PrintStoredDeviceValues()

	// 1. Apply conditional rules
	for _, rule := range writeMap.Cond {
		// Get the current value of the BIT device from storage
		bitVal, exists := s.GetDeviceValue(rule.Bit)
		if !exists {
			//s.logger.Printf("Warning: bit device value not found for %s", rule.Bit)
			continue
		}

		bitInt, ok := toInt(bitVal)
		if !ok {
			s.logger.Printf("Warning: cannot convert value to int for bit device %s: %v", rule.Bit, bitVal)
			continue
		}

		// Evaluate condition based on the BIT device value
		match := false
		switch rule.Operator {
		case "==":
			match = bitInt == 1
		case "!=":
			match = bitInt != 1
		}

		if match {
			// If condition matches, write the SRC device to its target
			if target, ok := writeMap.Default[rule.Src]; ok {
				// Get the current value of the SRC device to write
				srcVal, exists := s.GetDeviceValue(rule.Src)
				if !exists {
					//	s.logger.Printf("Warning: source value not found for %s", rule.Src)
					continue
				}

				//	s.logger.Printf("Conditional write: %s value: %v (triggered by %s=%v)", rule.Src, srcVal, rule.Bit, bitVal)
				if err := s.WriteDevice(ctx, target.PLCName, target.Device, srcVal); err != nil {
					return err
				}
				written[rule.Src] = true

				// Clear the stored value after successful write
				s.ClearDeviceValue(rule.Src)
			}
		}
	}

	// 2. Apply default write for non-conditional devices
	if !conditionalDevices[addr] && !written[addr] {
		if target, ok := writeMap.Default[addr]; ok {
			intVal, ok := toInt(value)
			if !ok {
				// cannot convert, skip write
				return nil
			}
			if intVal != 0 {
				//s.logger.Printf("Default write: %s value: %v", addr, value)
				return s.WriteDevice(ctx, target.PLCName, target.Device, value)
			}
		}
	}

	return nil
}
