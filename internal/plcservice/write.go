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
	// PLCName is the PLC whose WriteMap this rule guards. A rule only
	// releases targets on that PLC, so a SEC_TO_PRIM condition can never
	// trigger a PRIM_TO_SEC write for the same source address.
	PLCName string
}

type WriteMapWithCond struct {
	// Default maps a source address to every target it should be copied to.
	// A slice, so "D3026>D,1,1,1;D3026>D,2,1,1" writes both D1 and D2
	// instead of the second pair silently overwriting the first.
	Default map[string][]WriteTarget
	Cond    []SimpleCondWrite
	// Gated marks "<plc>|<src>" pairs that may only be written through a
	// matching condition rule. Every source of a PLC that has a CondMap is
	// gated — a source with no rule is never written, instead of silently
	// falling back to an unconditional write.
	Gated map[string]bool
}

func gateKey(plcName, src string) string { return plcName + "|" + src }

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

// BuildWriteMap parses every PLC's WriteMap and CondMap.
func BuildWriteMap(cfg config.AppConfig) WriteMapWithCond {
	defaultMap := make(map[string][]WriteTarget)
	gated := make(map[string]bool)
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

				defaultMap[src] = append(defaultMap[src], WriteTarget{
					PLCName: plcCfg.Name,
					Device:  device,
				})
			}
		}
		rules := ParseCondRules(plcCfg.CondMap)
		if len(rules) > 0 {
			ruled := make(map[string]bool)
			for i := range rules {
				rules[i].PLCName = plcCfg.Name
				ruled[rules[i].Src] = true
			}
			for src, targets := range defaultMap {
				for _, t := range targets {
					if t.PLCName != plcCfg.Name {
						continue
					}
					gated[gateKey(plcCfg.Name, src)] = true
					if !ruled[src] {
						log.Printf("⚠️ write map for PLC %q: source %s has a target but no condition rule — it will NOT be written (add e.g. Mxx==%s to its _CONDITION)", plcCfg.Name, src, src)
					}
					break
				}
			}
		}
		condRules = append(condRules, rules...)
	}

	return WriteMapWithCond{Default: defaultMap, Cond: condRules, Gated: gated}
}

// DirectWrite routes one read value to its write targets, honouring condition rules.
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

	// Workers call DirectWrite concurrently. The store → check → write →
	// clear sequence below must be atomic, or two workers can both see the
	// same stored value and write it twice.
	s.condMu.Lock()
	defer s.condMu.Unlock()

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
			targets, ok := writeMap.Default[rule.Src]
			if !ok {
				continue
			}
			srcVal, exists := s.GetDeviceValue(rule.Src)
			if !exists {
				continue
			}
			for _, target := range targets {
				if rule.PLCName != "" && target.PLCName != rule.PLCName {
					continue
				}
				if err := s.WriteDevice(ctx, target.PLCName, target.Device, srcVal); err != nil {
					return err
				}
			}
			written[rule.Src] = true
			s.ClearDeviceValue(rule.Src)
		}
	}

	if !conditionalDevices[addr] && !written[addr] {
		targets, ok := writeMap.Default[addr]
		if !ok {
			return nil
		}
		intVal, ok := toInt(value)
		if !ok {
			return nil
		}
		for _, target := range targets {
			// A PLC with a _CONDITION never accepts unconditional writes.
			if writeMap.Gated[gateKey(target.PLCName, addr)] {
				continue
			}
			// Word targets: zero values are not synced (existing behaviour).
			// Bit targets (type 3) mirror the source, OFF included —
			// otherwise X3E>M70 would latch M70 ON forever.
			if intVal == 0 && target.Device.ProcessNumber != 3 {
				continue
			}
			if err := s.WriteDevice(ctx, target.PLCName, target.Device, value); err != nil {
				return err
			}
		}
	}

	return nil
}
