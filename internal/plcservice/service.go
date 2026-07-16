package plcservice

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/plc"
	"github.com/mochigome-git/msp-go/pkg/plc/mitsubishi"
	"github.com/mochigome-git/msp-go/pkg/plc/shibaura"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

type Service struct {
	clients      map[string]plc.PLCClient
	devices      map[string][]PLC_Utils.Device
	fx           map[string]bool // ← per-PLC, not a single bool
	deviceValues map[string]any
	logger       *log.Logger
	mu           sync.Mutex
	valuesMutex  sync.RWMutex
}

func NewService(logger *log.Logger) *Service {
	return &Service{
		clients:      make(map[string]plc.PLCClient),
		devices:      make(map[string][]PLC_Utils.Device),
		fx:           make(map[string]bool),
		deviceValues: make(map[string]any),
		logger:       logger,
	}
}

func (s *Service) InitPLC(cfg config.PLCConfig, deviceStrs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var client plc.PLCClient
	switch strings.ToLower(strings.TrimSpace(cfg.Brand)) {
	case "shibaura":
		client = shibaura.NewClient(cfg.Host, cfg.Port, 1)
	default: // "mitsubishi" or empty
		c, err := mitsubishi.NewMSPClient(cfg.Host, cfg.Port)
		if err != nil {
			return fmt.Errorf("failed to connect to PLC %s: %w", cfg.Name, err)
		}
		client = c
	}

	s.clients[cfg.Name] = client
	s.fx[cfg.Name] = cfg.FxModel // ← per-PLC now

	for _, devStr := range deviceStrs {
		devStr = strings.TrimSpace(devStr)
		if devStr == "" {
			continue
		}
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
			s.devices[cfg.Name] = append(s.devices[cfg.Name], PLC_Utils.Device{
				DeviceType:      deviceType,
				DeviceNumber:    deviceNumber,
				NumberRegisters: uint16(numRegs),
			})
		}
	}

	s.logger.Printf("PLC %s initialized at %s:%d brand=%s fx=%v devices=%d",
		cfg.Name, cfg.Host, cfg.Port, cfg.Brand, cfg.FxModel, len(s.devices[cfg.Name]))
	return nil
}

// FX returns the fx flag for a specific PLC by name.
// Replace all uses of s.fx with s.FX("main") / s.FX("secondary").
func (s *Service) FX(plcName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fx[plcName]
}

// Client returns the PLCClient for a named PLC.
func (s *Service) Client(plcName string) (plc.PLCClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[plcName]
	if !ok {
		return nil, fmt.Errorf("plcservice: no PLC named %q", plcName)
	}
	return c, nil
}

// Devices returns registered devices for a named PLC.
func (s *Service) Devices(plcName string) []PLC_Utils.Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devices[plcName]
}

func (s *Service) StoreDeviceValue(address string, value any) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.deviceValues[address] = value
}

func (s *Service) GetDeviceValue(address string) (any, bool) {
	s.valuesMutex.RLock()
	defer s.valuesMutex.RUnlock()
	value, exists := s.deviceValues[address]
	return value, exists
}

func (s *Service) ClearDeviceValue(address string) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	delete(s.deviceValues, address)
}

func (s *Service) ClearAllDeviceValues() {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.deviceValues = make(map[string]any)
}

func (s *Service) Close() {
	for plcName := range s.clients {
		s.logger.Printf("Closing PLC client %s", plcName)
	}
}
