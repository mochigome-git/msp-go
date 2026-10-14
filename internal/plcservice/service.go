package plcservice

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mochigome-git/msp-go/pkg/config"
	"github.com/mochigome-git/msp-go/pkg/plc"
	"github.com/mochigome-git/msp-go/pkg/plc/mitsubishi"
	"github.com/mochigome-git/msp-go/pkg/plc/shibaura"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

type Service struct {
	clients      map[string]plc.PLCClient
	devices      map[string][]PLC_Utils.Device
	fx           map[string]bool                  // ← per-PLC, not a single bool
	plans        map[string]*mitsubishi.BlockPlan // block-read plan per PLC (nil = per-device reads)
	down         map[string]bool                  // PLC currently unreachable
	downUntil    map[string]time.Time             // skip reads until this time
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
		plans:        make(map[string]*mitsubishi.BlockPlan),
		down:         make(map[string]bool),
		downUntil:    make(map[string]time.Time),
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

	if _, ok := client.(blockReader); ok && cfg.BlockRead {
		plan := mitsubishi.PlanBlocks(s.devices[cfg.Name], cfg.FxModel,
			mitsubishi.PlanOptions{MaxGapWords: cfg.BlockMaxGap})
		s.plans[cfg.Name] = plan
		s.logPlan(cfg.Name, plan)
	}
	return nil
}

// blockReader is implemented by clients that support batch block reads
// (currently mitsubishi.MSPClient). PLCClient is unchanged, so Shibaura
// and future brands are unaffected.
type blockReader interface {
	ReadBlock(ctx context.Context, b *mitsubishi.Block) ([]mitsubishi.BlockValue, error)
}

func (s *Service) logPlan(plcName string, p *mitsubishi.BlockPlan) {
	s.logger.Printf("PLC %s block read: %d devices -> %d requests/cycle (%d blocks + %d single reads)",
		plcName, len(s.devices[plcName]), p.Requests(), len(p.Blocks), len(p.Singles))
	for _, b := range p.Blocks {
		s.logger.Printf("  block %s%d x%d words, %d devices", b.DeviceType, b.Start, b.Words, len(b.Members))
	}
	for _, d := range p.Singles {
		s.logger.Printf("  single %s%s (type %d)", d.DeviceType, d.DeviceNumber, d.NumberRegisters)
	}
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
