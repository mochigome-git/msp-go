package plcservice

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// toInt converts any input value to 0 or 1 for conditional checks
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

// ParseCondMap parses env string like "M64==D71,M30==D80" into a map[condition]source
func ParseCondRules(str string) []SimpleCondWrite {
	var rules []SimpleCondWrite
	str = strings.TrimSpace(str)
	if str == "" {
		return rules
	}

	for _, e := range strings.Split(str, ";") {
		e = strings.TrimSpace(e)
		var operator string
		if strings.Contains(e, "==") {
			operator = "=="
		} else if strings.Contains(e, "!=") {
			operator = "!="
		} else {
			continue
		}

		parts := strings.Split(e, operator)
		if len(parts) != 2 {
			continue
		}

		rules = append(rules, SimpleCondWrite{
			Bit:      strings.TrimSpace(parts[0]),
			Operator: operator,
			Src:      strings.TrimSpace(parts[1]),
		})
	}

	return rules
}

// PrintStoredDeviceValues prints all stored device values with a timestamp
func (s *Service) PrintStoredDeviceValues() {
	s.valuesMutex.RLock()
	defer s.valuesMutex.RUnlock()

	if len(s.deviceValues) == 0 {
		s.logger.Println("No device values stored")
		return
	}

	fmt.Printf("[%s] Stored device values (%d):\n", time.Now().Format("2006-01-02 15:04:05"), len(s.deviceValues))
	for addr, val := range s.deviceValues {
		fmt.Printf("  %s => %v\n", addr, val)
	}
}
