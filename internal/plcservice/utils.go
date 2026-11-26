package plcservice

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// toInt converts a read value to an int for conditional checks
func toInt(val any) (int, bool) {
	switch v := val.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i, true
		}
		return 0, false
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// ParseCondRules parses a *_CONDITION value: "M64==D71;M30!=D80".
//
// Each rule is BIT==SRC or BIT!=SRC, where BIT and SRC are addresses as
// they are read (no commas: "M64", not "M,64"):
//
//	M64==D71  write D71 to its targets while M64 is 1
//	M30!=D80  write D80 to its targets while M30 is not 1
func ParseCondRules(str string) ([]SimpleCondWrite, error) {
	var rules []SimpleCondWrite
	// Strip surrounding quotes that can leak through compose/env interpolation,
	// e.g. "M64==D71;M80==D71" arriving with the literal quotes.
	str = strings.Trim(strings.TrimSpace(str), `"'`)
	if str == "" {
		return rules, nil
	}

	for _, e := range strings.Split(str, ";") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		var operator string
		switch {
		case strings.Contains(e, "=="):
			operator = "=="
		case strings.Contains(e, "!="):
			operator = "!="
		default:
			return nil, fmt.Errorf("condition %q: expected BIT==SRC or BIT!=SRC", e)
		}

		parts := strings.Split(e, operator)
		if len(parts) != 2 {
			return nil, fmt.Errorf("condition %q: expected exactly one %s", e, operator)
		}
		bit, src := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if !isAddress(bit) || !isAddress(src) {
			return nil, fmt.Errorf("condition %q: both sides must be device addresses like M64 or D71 (no commas, no literal values)", e)
		}
		rules = append(rules, SimpleCondWrite{Bit: bit, Operator: operator, Src: src})
	}
	return rules, nil
}

// isAddress reports whether s looks like a read address: a device letter
// followed by letters/digits, e.g. "M64", "D71", "X3E". It rejects literal
// values ("1") and the DEVICES-style "M,64".
func isAddress(s string) bool {
	if len(s) < 2 || !isLetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isLetter(s[i]) && (s[i] < '0' || s[i] > '9') {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

// PrintStoredDeviceValues prints all stored device values with a timestamp
func (s *Service) PrintStoredDeviceValues() {
	s.valuesMutex.RLock()
	defer s.valuesMutex.RUnlock()

	if len(s.deviceValues) == 0 {
		s.logger.Println("No device values stored")
		return
	}

	fmt.Printf("[%s] Stored device values (%d):\n", time.Now().Format("2006-01-02 15:04:05"), len(s.deviceValues))
	for addr, v := range s.deviceValues {
		fmt.Printf("  %s => %v (from %s, %s ago)\n", addr, v.value, v.source, time.Since(v.at).Round(time.Millisecond))
	}
}
