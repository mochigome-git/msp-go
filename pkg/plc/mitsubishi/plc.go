// Package mitsubishi implements PLCClient for Mitsubishi Electric PLCs
// using the MC Protocol (SLMP): 3E frame over TCP, or 1E frame for FX series.
package mitsubishi

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"unicode"

	"github.com/mochigome-git/msp-go/pkg/mcp"
)

// MSPClient wraps the MC Protocol client for Mitsubishi PLCs.
type MSPClient struct {
	client  mcp.Client
	textEnc TextEncoding // how data type 4 registers are decoded
}

// SetTextEncoding sets how data type 4 (text) registers are decoded.
func (m *MSPClient) SetTextEncoding(enc TextEncoding) { m.textEnc = enc }

// NewMSPClient creates a new Mitsubishi MC Protocol client.
func NewMSPClient(plcHost string, plcPort int) (*MSPClient, error) {
	client, err := mcp.New3EClient(plcHost, plcPort, mcp.NewLocalStation())
	if err != nil {
		return nil, err
	}
	return &MSPClient{client: client}, nil
}

// ReadData reads data from the Mitsubishi PLC for the specified device.
func (m *MSPClient) ReadData(ctx context.Context, deviceType string, deviceNumber string, numberRegisters uint16, fx bool) (any, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("MSP client not initialized")
	}

	// X, Y, B, W are hex-addressed (see parseDeviceNumber).
	deviceNumberInt64, err := parseDeviceNumber(deviceType, deviceNumber)
	if err != nil {
		return nil, err
	}

	resultCh := make(chan any, 1) // buffered: goroutine must not block forever if ctx times out first
	errCh := make(chan error, 1)

	go func() {
		data, err := m.client.Read(deviceType, deviceNumberInt64, int64(numberRegisters), fx)
		if err != nil {
			errCh <- err
			return
		}
		value, err := parseData(data, int(numberRegisters), fx, m.textEnc)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- value
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case value := <-resultCh:
		return value, nil
	}
}

// WriteData sends data to the Mitsubishi PLC for the specified device.
func (m *MSPClient) WriteData(deviceType, deviceNumber string, writeData []byte, numberRegisters uint16) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("MSP client not initialized")
	}

	deviceNumberInt64, err := parseDeviceNumber(deviceType, deviceNumber)
	if err != nil {
		return err
	}

	// A single bit value (EncodeData type 3 → 1 byte) on a bit device is
	// written in BIT units, so only that one point changes. Writing it in
	// word units would overwrite the next 15 points too (M70 → M70..M85).
	if bitDevices[deviceType] && len(writeData) == 1 {
		_, err = m.client.WriteBits(deviceType, deviceNumberInt64, []byte{writeData[0] & 0x01})
		return err
	}

	calculatedRegisters := (len(writeData) + 1) / 2
	if numberRegisters == 0 || int(numberRegisters) < calculatedRegisters {
		numberRegisters = uint16(calculatedRegisters)
	}

	_, err = m.client.Write(deviceType, deviceNumberInt64, int64(numberRegisters), writeData)
	return err
}

// BatchWrite writes writeData starting at startDevice, split into requests
// of at most maxRegistersPerWrite words. logger may be nil.
func (m *MSPClient) BatchWrite(deviceType, startDevice string, writeData []byte, maxRegistersPerWrite uint16, logger *log.Logger) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("MSP client not initialized")
	}

	// X, Y, B, W are hex-addressed. Parse by device type, never
	// decimal-first-then-fallback: "10" is valid in both bases but means
	// different devices.
	val64, err := parseDeviceNumber(deviceType, startDevice)
	if err != nil {
		return err
	}
	deviceNumberUint16 := uint16(val64)

	totalRegisters := (len(writeData) + 1) / 2
	written := 0

	for written < totalRegisters {
		remaining := totalRegisters - written
		chunkSize := remaining
		if chunkSize > int(maxRegistersPerWrite) {
			chunkSize = int(maxRegistersPerWrite)
		}

		startIndex := written * 2
		endIndex := startIndex + chunkSize*2
		if endIndex > len(writeData) {
			endIndex = len(writeData)
		}
		chunk := writeData[startIndex:endIndex]

		addr := deviceNumberUint16 + uint16(written) // forward from the real start, no top-of-range shift

		if logger != nil {
			logger.Printf("Writing to %s device number %d, chunk size %d, data % X\n", deviceType, addr, chunkSize, chunk)
		}

		if _, err := m.client.Write(deviceType, int64(addr), int64(chunkSize), chunk); err != nil {
			return err
		}
		written += chunkSize
	}

	return nil
}

// IncrementDevice increments a device string like "W10" to "W11".
func IncrementDevice(device string, offset int64) (string, error) {
	var prefix string
	var numStr string

	for i, r := range device {
		if unicode.IsDigit(r) || (r >= 'A' && r <= 'F') {
			prefix = device[:i]
			numStr = device[i:]
			break
		}
	}
	if prefix == "" {
		numStr = device
	}

	base := 10
	if hexDevices[prefix] {
		base = 16
	}
	num, err := strconv.ParseInt(numStr, base, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse device number part: %w", err)
	}
	num += offset

	var newNumStr string
	if base == 16 {
		newNumStr = fmt.Sprintf("%X", num)
	} else {
		newNumStr = fmt.Sprintf("%d", num)
	}
	return prefix + newNumStr, nil
}

func (m *MSPClient) EncodeData(valueStr string, processNumber int) ([]byte, error) {
	return EncodeData(valueStr, processNumber)
}

// Close closes the TCP connection to the PLC.
func (m *MSPClient) Close() error {
	if m == nil || m.client == nil {
		return nil
	}
	return m.client.Close()
}
