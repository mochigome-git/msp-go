package plcservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/plc/mitsubishi"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

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

// WriteDevice encodes value with the target's write type and writes it.
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
			if device.NumberRegisters == 1 {
				done <- client.WriteData(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters)
			} else {
				done <- client.BatchWrite(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters, nil)
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

// ParseWriteMap parses every PLC's WriteMap and CondMap.
//
// WriteMap entries are "SRC>TYPE,NUMBER[,WRITETYPE[,REGS]]" separated by
// ';', e.g. "D71>D,3026,1,1;X3E>M,70,3,1": copy the value read at SRC into
// TYPE+NUMBER on the PLC that owns this WriteMap. WRITETYPE (default 1) is
// the EncodeData type: 1 uint16, 2 float32, 3 bit, 4 ASCII, 5 int16, 6 FX x10.
// REGS (default 1) is registers per write request.
//
// Any malformed entry is an error, so a typo stops startup instead of
// silently dropping (or misdirecting) a write.
func ParseWriteMap(cfg config.AppConfig) (WriteMapWithCond, error) {
	defaultMap := make(map[string][]WriteTarget)
	gated := make(map[string]bool)
	var condRules []SimpleCondWrite
	var errs []error

	for _, plcCfg := range cfg.PLCs {
		mapStr := strings.Trim(strings.TrimSpace(plcCfg.WriteMap), `"'`)
		for _, pair := range strings.Split(mapStr, ";") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			device, src, err := parseWriteEntry(pair)
			if err != nil {
				errs = append(errs, fmt.Errorf("write map for PLC %q: %w", plcCfg.Name, err))
				continue
			}
			defaultMap[src] = append(defaultMap[src], WriteTarget{PLCName: plcCfg.Name, Device: device})
		}

		rules, err := ParseCondRules(plcCfg.CondMap)
		if err != nil {
			errs = append(errs, fmt.Errorf("write condition for PLC %q: %w", plcCfg.Name, err))
			continue
		}
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
			for _, r := range rules {
				if !hasTargetOn(defaultMap[r.Src], plcCfg.Name) {
					log.Printf("⚠️ write condition for PLC %q: %s%s%s — %s has no target in this PLC's write map, rule does nothing", plcCfg.Name, r.Bit, r.Operator, r.Src, r.Src)
				}
			}
		}
		condRules = append(condRules, rules...)
	}

	return WriteMapWithCond{Default: defaultMap, Cond: condRules, Gated: gated}, errors.Join(errs...)
}

func hasTargetOn(targets []WriteTarget, plcName string) bool {
	for _, t := range targets {
		if t.PLCName == plcName {
			return true
		}
	}
	return false
}

func parseWriteEntry(pair string) (PLC_Utils.Device, string, error) {
	src, dest, ok := strings.Cut(pair, ">")
	src, dest = strings.TrimSpace(src), strings.TrimSpace(dest)
	if !ok || !isAddress(src) || dest == "" {
		return PLC_Utils.Device{}, "", fmt.Errorf("entry %q: expected SRC>TYPE,NUMBER[,WRITETYPE[,REGS]] (e.g. D100>D,200,1,1)", pair)
	}
	parts := strings.Split(dest, ",")
	if len(parts) < 2 || len(parts) > 4 {
		return PLC_Utils.Device{}, "", fmt.Errorf("entry %q: target must be TYPE,NUMBER[,WRITETYPE[,REGS]]", pair)
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	for len(parts) < 4 {
		parts = append(parts, "1")
	}
	processNumber, err1 := strconv.ParseUint(parts[2], 10, 16)
	numRegs, err2 := strconv.ParseUint(parts[3], 10, 16)
	if parts[0] == "" || parts[1] == "" || err1 != nil || err2 != nil {
		return PLC_Utils.Device{}, "", fmt.Errorf("entry %q: bad target %q", pair, dest)
	}
	return PLC_Utils.Device{
		DeviceType:      parts[0],
		DeviceNumber:    parts[1],
		NumberRegisters: uint16(numRegs),
		ProcessNumber:   uint16(processNumber),
	}, src, nil
}

// ValidateWriteMap checks the parsed targets against the configured PLCs:
// every target PLC must be initialised and writable, and every target
// address valid for it. Sources and condition bits that no PLC reads are
// reported as warnings, since such a rule can never fire.
func (s *Service) ValidateWriteMap(cfg config.AppConfig, wm WriteMapWithCond) error {
	plcCfgs := make(map[string]config.PLCConfig)
	for _, p := range cfg.PLCs {
		plcCfgs[p.Name] = p
	}

	read := make(map[string]bool)
	s.mu.Lock()
	for _, devs := range s.devices {
		for _, d := range devs {
			read[d.DeviceType+d.DeviceNumber] = true
		}
	}
	s.mu.Unlock()

	var errs []error
	for src, targets := range wm.Default {
		if !read[src] {
			s.logger.Printf("⚠️ write map: source %s is not in any DEVICES_* list, it will never be written", src)
		}
		for _, t := range targets {
			if _, err := s.Client(t.PLCName); err != nil {
				errs = append(errs, fmt.Errorf("write map: %s -> %s%s on PLC %q, but that PLC is not configured (host empty?)", src, t.Device.DeviceType, t.Device.DeviceNumber, t.PLCName))
				continue
			}
			pc := plcCfgs[t.PLCName]
			switch strings.ToLower(strings.TrimSpace(pc.Brand)) {
			case "", "mitsubishi":
				if err := mitsubishi.ValidateWriteDevice(t.Device, pc.FxModel); err != nil {
					errs = append(errs, fmt.Errorf("write map: %s -> PLC %q: %w", src, t.PLCName, err))
				}
			default:
				errs = append(errs, fmt.Errorf("write map: %s -> PLC %q: writes to brand %q are not supported", src, t.PLCName, pc.Brand))
			}
		}
	}
	for _, r := range wm.Cond {
		if !read[r.Bit] {
			s.logger.Printf("⚠️ write condition %s%s%s: %s is not in any DEVICES_* list, the condition can never be evaluated", r.Bit, r.Operator, r.Src, r.Bit)
		}
	}
	return errors.Join(errs...)
}

// DirectWrite routes one read value to its write targets, honouring
// condition rules. A failing target does not stop the remaining targets;
// all failures are returned together.
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
	source, _ := msg["source"].(string)

	// Workers call DirectWrite concurrently. The store → check → write →
	// clear sequence below must be atomic, or two workers can both see the
	// same stored value and write it twice.
	s.condMu.Lock()
	defer s.condMu.Unlock()

	var errs []error
	written := make(map[string]bool)
	conditionalDevices := make(map[string]bool)

	for _, rule := range writeMap.Cond {
		conditionalDevices[rule.Src] = true
		conditionalDevices[rule.Bit] = true
	}
	if conditionalDevices[addr] {
		s.StoreDeviceValue(addr, value, source)
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
		if !match {
			continue
		}
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
				errs = append(errs, fmt.Errorf("%s -> %s %s%s: %w", rule.Src, target.PLCName, target.Device.DeviceType, target.Device.DeviceNumber, err))
			}
		}
		written[rule.Src] = true
		s.ClearDeviceValue(rule.Src)
	}

	if !conditionalDevices[addr] && !written[addr] {
		targets, ok := writeMap.Default[addr]
		if !ok {
			return errors.Join(errs...)
		}
		intVal, ok := toInt(value)
		if !ok {
			return errors.Join(errs...) // existing behaviour: non-numeric values only sync via a condition
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
				errs = append(errs, fmt.Errorf("%s -> %s %s%s: %w", addr, target.PLCName, target.Device.DeviceType, target.Device.DeviceNumber, err))
			}
		}
	}

	return errors.Join(errs...)
}
