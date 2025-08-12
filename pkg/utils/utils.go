package utils

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Define the device struct with the address field
type Device struct {
	DeviceType      string
	DeviceNumber    string
	ProcessNumber   uint16
	NumberRegisters uint16
}

// ParseDeviceAddresses parses the device addresses from the environment variable.
func ParseDeviceAddresses(envVar string, logger *log.Logger) ([]Device, error) {
	deviceStrings := strings.Split(envVar, ",")
	if len(deviceStrings)%3 != 0 {
		logger.Printf("Invalid DEVICES environment variable: %s", envVar)
		return nil, nil
	}
	var devices []Device
	for i := 0; i < len(deviceStrings); i += 3 {
		deviceNumber := deviceStrings[i+1]

		numberRegisters, err := strconv.ParseUint(deviceStrings[i+2], 10, 16)
		if err != nil {
			logger.Fatalf("Error parsing number of registers: %v", err)
		}
		devices = append(devices, Device{
			DeviceType:      deviceStrings[i],
			DeviceNumber:    fmt.Sprint(deviceNumber),
			NumberRegisters: uint16(numberRegisters),
		})
	}
	if len(devices) == 0 {
		logger.Fatalf("No devices found in DEVICES environment variable: %s", envVar)
	}
	logger.Printf("Loaded %d device(s) from DEVICES environment variable", len(devices))
	return devices, nil
}

func ParseWriteDeviceAddresses(upsertStr string) ([]Device, error) {
	parts := strings.Split(upsertStr, ",")
	if len(parts)%4 != 0 {
		return nil, fmt.Errorf("invalid device upsert format")
	}

	devices := make([]Device, 0, len(parts)/4)
	for i := 0; i < len(parts); i += 4 {
		numRegs, err := strconv.ParseUint(strings.TrimSpace(parts[i+2]), 10, 16)
		if err != nil {
			return nil, err
		}
		maxRegs, err := strconv.ParseUint(strings.TrimSpace(parts[i+3]), 10, 16)
		if err != nil {
			return nil, err
		}
		devices = append(devices, Device{
			DeviceType:      strings.TrimSpace(parts[i]),
			DeviceNumber:    strings.TrimSpace(parts[i+1]),
			ProcessNumber:   uint16(numRegs),
			NumberRegisters: uint16(maxRegs),
		})
	}
	return devices, nil
}
