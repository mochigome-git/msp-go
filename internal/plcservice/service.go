package plcservice

import (
	"log"
	"strconv"
	"strings"
	"sync"

	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// Service manages multiple PLC clients and devices
type Service struct {
	clients      map[string]*plcWrapper        // PLC name -> plcWrapper
	devices      map[string][]PLC_Utils.Device // PLC name -> devices
	deviceValues map[string]any                // Store current values of all devices (address -> value)
	logger       *log.Logger
	fx           bool
	mu           sync.Mutex
	valuesMutex  sync.RWMutex // Separate mutex for device values
}

// NewService creates a new PLC service
func NewService(logger *log.Logger) *Service {
	return &Service{
		clients:      make(map[string]*plcWrapper),
		devices:      make(map[string][]PLC_Utils.Device),
		deviceValues: make(map[string]any),
		logger:       logger,
	}
}

// InitPLC initializes a PLC client and registers its devices
func (s *Service) InitPLC(name, host string, port int, deviceStrs []string, fx bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[name] = &plcWrapper{host: host, port: port}

	for _, devStr := range deviceStrs {
		devStr = strings.TrimSpace(devStr)
		if devStr == "" {
			continue
		}

		// Split by comma, should be multiples of 3: Type, Number, Registers
		parts := strings.Split(devStr, ",")
		if len(parts)%3 != 0 {
			s.logger.Printf("Skipping invalid device string: '%s'", devStr)
			continue
		}

		for i := 0; i < len(parts); i += 3 {
			deviceType := strings.TrimSpace(parts[i])
			deviceNumber := strings.TrimSpace(parts[i+1])
			numRegs := 1
			if n, err := strconv.Atoi(strings.TrimSpace(parts[i+2])); err == nil {
				numRegs = n
			} else {
				s.logger.Printf("Invalid number of registers '%s', defaulting to 1", parts[i+2])
			}

			s.devices[name] = append(s.devices[name], PLC_Utils.Device{
				DeviceType:      deviceType,
				DeviceNumber:    deviceNumber,
				NumberRegisters: uint16(numRegs),
			})
		}
	}

	s.fx = fx
	s.logger.Printf("PLC %s initialized at %s:%d with %d devices", name, host, port, len(s.devices[name]))
	return nil
}

// StoreDeviceValue stores the current value of a device
func (s *Service) StoreDeviceValue(address string, value any) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.deviceValues[address] = value
	//s.logger.Printf("Stored device value: %s = %v", address, value)
}

// GetDeviceValue retrieves the current value of a device
func (s *Service) GetDeviceValue(address string) (any, bool) {
	s.valuesMutex.RLock()
	defer s.valuesMutex.RUnlock()
	value, exists := s.deviceValues[address]
	return value, exists
}

// ClearDeviceValue removes a device value from storage
func (s *Service) ClearDeviceValue(address string) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	delete(s.deviceValues, address)
	// s.logger.Printf("Cleared device value: %s", address)
}

// ClearAllDeviceValues clears all stored device values
func (s *Service) ClearAllDeviceValues() {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.deviceValues = make(map[string]any)
	// s.logger.Printf("Cleared all device values")
}

// Close disconnects all PLC clients (optional)
func (s *Service) Close() {
	for plcName := range s.clients {
		s.logger.Printf("Closing PLC client %s", plcName)
		// currently plc package doesn't expose Close(), but placeholder if needed
	}
}
