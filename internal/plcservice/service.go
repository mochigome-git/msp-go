package plcservice

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/mochigome-git/msp-go/pkg/plc"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// plcWrapper wraps the global plc package for a specific PLC host/port
type plcWrapper struct {
	host   string
	port   int
	mu     sync.Mutex
	client *plc.MSPClient
}

func (p *plcWrapper) initClient() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return nil
	}

	client, err := plc.NewMSPClient(p.host, p.port)
	if err != nil {
		return err
	}
	p.client = client
	return nil
}

func (p *plcWrapper) Read(ctx context.Context, device PLC_Utils.Device, fx bool) (any, error) {
	if err := p.initClient(); err != nil {
		return nil, err
	}
	return p.client.ReadData(ctx, device.DeviceType, device.DeviceNumber, device.NumberRegisters, fx)
}

func (p *plcWrapper) Write(device PLC_Utils.Device, data []byte) error {
	if err := p.initClient(); err != nil {
		return err
	}
	if device.NumberRegisters == 1 {
		return p.client.WriteData(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters)
	}
	return p.client.BatchWrite(device.DeviceType, device.DeviceNumber, data, device.NumberRegisters, log.Default())
}

// Service manages multiple PLC clients and devices
type Service struct {
	clients map[string]*plcWrapper        // PLC name -> plcWrapper
	devices map[string][]PLC_Utils.Device // PLC name -> devices
	logger  *log.Logger
	fx      bool
	mu      sync.Mutex
}

// NewService creates a new PLC service
func NewService(logger *log.Logger) *Service {
	return &Service{
		clients: make(map[string]*plcWrapper),
		devices: make(map[string][]PLC_Utils.Device),
		logger:  logger,
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

// Close disconnects all PLC clients (optional)
func (s *Service) Close() {
	for plcName := range s.clients {
		s.logger.Printf("Closing PLC client %s", plcName)
		// currently plc package doesn't expose Close(), but placeholder if needed
	}
}
