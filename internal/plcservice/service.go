package plcservice

import (
	"context"
	"errors"
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
	fx           map[string]bool                  // per-PLC FX (1E frame) flag
	plans        map[string]*mitsubishi.BlockPlan // block-read plan per PLC (nil = per-device reads)
	down         map[string]bool                  // PLC currently unreachable
	downUntil    map[string]time.Time             // skip reads until this time
	deviceValues map[string]storedValue           // latest values used by write conditions
	valueMaxAge  time.Duration                    // stored values older than this are ignored
	logger       *log.Logger
	mu           sync.Mutex
	valuesMutex  sync.RWMutex
	condMu       sync.Mutex // serializes DirectWrite's condition evaluation
}

func NewService(logger *log.Logger) *Service {
	return &Service{
		clients:      make(map[string]plc.PLCClient),
		devices:      make(map[string][]PLC_Utils.Device),
		fx:           make(map[string]bool),
		plans:        make(map[string]*mitsubishi.BlockPlan),
		down:         make(map[string]bool),
		downUntil:    make(map[string]time.Time),
		deviceValues: make(map[string]storedValue),
		valueMaxAge:  defaultValueMaxAge,
		logger:       logger,
	}
}

// storedValue is a cached read used to evaluate write conditions.
type storedValue struct {
	value  any
	source string // PLC the value was read from ("" if unknown)
	at     time.Time
}

// defaultValueMaxAge: a condition bit or source value that hasn't been
// refreshed for this long is treated as unknown, so a stale "machine
// running" bit can't keep authorising writes.
const defaultValueMaxAge = 30 * time.Second

// SetPollInterval adapts the stale-value limit to the polling rate: values
// must be at least a few cycles old before they are considered stale.
func (s *Service) SetPollInterval(d time.Duration) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.valueMaxAge = max(defaultValueMaxAge, 3*d)
}

func (s *Service) InitPLC(cfg config.PLCConfig, deviceStrs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// An empty host usually means the env vars never reached the container
	// (e.g. SEC_PLC_HOST not mapped in docker-compose). Skip loudly instead
	// of creating a client that silently dials localhost.
	if strings.TrimSpace(cfg.Host) == "" {
		s.logger.Printf("⚠️ PLC %s: host not set, skipping (check env mapping)", cfg.Name)
		return nil
	}

	var client plc.PLCClient
	cfg.Brand = strings.ToLower(strings.TrimSpace(cfg.Brand))
	switch cfg.Brand {
	case "shibaura":
		client = shibaura.NewClient(cfg.Host, cfg.Port, 1)
	case "", "mitsubishi":
		c, err := mitsubishi.NewMSPClient(cfg.Host, cfg.Port)
		if err != nil {
			return fmt.Errorf("failed to connect to PLC %s: %w", cfg.Name, err)
		}
		enc, err := mitsubishi.ParseTextEncoding(cfg.TextEncoding)
		if err != nil {
			s.logger.Printf("PLC %s: %v — using auto", cfg.Name, err)
		}
		c.SetTextEncoding(enc)
		client = c
	default:
		return fmt.Errorf("PLC %s: unknown brand %q (use mitsubishi or shibaura)", cfg.Name, cfg.Brand)
	}

	s.clients[cfg.Name] = client
	s.fx[cfg.Name] = cfg.FxModel

	isMitsubishi := cfg.Brand == "" || cfg.Brand == "mitsubishi"
	devices, err := parseDevices(deviceStrs)
	if err != nil {
		client.Close()
		return fmt.Errorf("PLC %s: %w", cfg.Name, err)
	}
	var errs []error
	for _, d := range devices {
		if isMitsubishi {
			if err := mitsubishi.ValidateReadDevice(d, cfg.FxModel); err != nil {
				errs = append(errs, err)
			}
		} else if d.NumberRegisters == 0 {
			errs = append(errs, fmt.Errorf("%s%s: count must be >= 1", d.DeviceType, d.DeviceNumber))
		}
	}
	if err := errors.Join(errs...); err != nil {
		client.Close()
		return fmt.Errorf("PLC %s: invalid DEVICES entry:\n%w", cfg.Name, err)
	}
	s.devices[cfg.Name] = devices

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

// HasDevices reports whether any initialised PLC has devices to read.
func (s *Service) HasDevices() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if len(d) > 0 {
			return true
		}
	}
	return false
}

// StoreDeviceValue caches a value read from PLC source.
func (s *Service) StoreDeviceValue(address string, value any, source string) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	s.deviceValues[address] = storedValue{value: value, source: source, at: time.Now()}
}

// GetDeviceValue returns a cached value, or false if it is missing or stale.
func (s *Service) GetDeviceValue(address string) (any, bool) {
	s.valuesMutex.RLock()
	defer s.valuesMutex.RUnlock()
	v, ok := s.deviceValues[address]
	if !ok || time.Since(v.at) > s.valueMaxAge {
		return nil, false
	}
	return v.value, true
}

func (s *Service) ClearDeviceValue(address string) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	delete(s.deviceValues, address)
}

// clearValuesFrom drops every cached value read from plcName, e.g. when
// that PLC becomes unreachable and its last values can no longer be trusted.
func (s *Service) clearValuesFrom(plcName string) {
	s.valuesMutex.Lock()
	defer s.valuesMutex.Unlock()
	for addr, v := range s.deviceValues {
		if v.source == plcName {
			delete(s.deviceValues, addr)
		}
	}
}

// Close closes every PLC connection.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, c := range s.clients {
		if err := c.Close(); err != nil {
			s.logger.Printf("Closing PLC client %s: %v", name, err)
		}
	}
}

// parseDevices parses DEVICES_* strings: "TYPE,NUMBER,N" triples, comma
// separated. Any malformed entry is an error.
func parseDevices(deviceStrs []string) ([]PLC_Utils.Device, error) {
	var devices []PLC_Utils.Device
	for _, devStr := range deviceStrs {
		devStr = strings.Trim(strings.TrimSpace(devStr), `"'`)
		if devStr == "" {
			continue
		}
		parts := strings.Split(devStr, ",")
		if len(parts)%3 != 0 {
			return nil, fmt.Errorf("device list %q: expected TYPE,NUMBER,N triples", devStr)
		}
		for i := 0; i < len(parts); i += 3 {
			deviceType := strings.TrimSpace(parts[i])
			deviceNumber := strings.TrimSpace(parts[i+1])
			n, err := strconv.ParseUint(strings.TrimSpace(parts[i+2]), 10, 16)
			if deviceType == "" || deviceNumber == "" || err != nil {
				return nil, fmt.Errorf("device list %q: bad entry %q", devStr, strings.Join(parts[i:i+3], ","))
			}
			devices = append(devices, PLC_Utils.Device{
				DeviceType:      deviceType,
				DeviceNumber:    deviceNumber,
				NumberRegisters: uint16(n),
			})
		}
	}
	return devices, nil
}
