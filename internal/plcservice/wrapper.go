package plcservice

import (
	"context"
	"log"
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
