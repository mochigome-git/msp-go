package reader

import (
	"context"
	"fmt"
	"msp-go/pkg/plc"
	"msp-go/pkg/utils"
)

type DeviceReader interface {
	Read(ctx context.Context) (interface{}, error)
}

type PLCDeviceReader struct {
	Device utils.Device
	FX     bool
}

func (r *PLCDeviceReader) Read(ctx context.Context) (interface{}, error) {
	value, err := plc.ReadData(ctx, r.Device.DeviceType, r.Device.DeviceNumber, r.Device.NumberRegisters, r.FX)
	if err != nil {
		return nil, fmt.Errorf("read failed for device %s%s: %w", r.Device.DeviceType, r.Device.DeviceNumber, err)
	}
	return value, nil
}
