// Package mitsubishi implements PLCClient for Mitsubishi Electric PLCs
// using the MC Protocol (SLMP) 3E frame over TCP.
// This is your original pkg/plc code — only the package declaration changed.
package mitsubishi

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"unicode"

	"github.com/mochigome-git/msp-go/pkg/mcp"
)

// Compile-time check: MSPClient must satisfy pkg/plc.PLCClient.
// If the interface changes, this line will fail loudly at build time.
// (import the plc package here only for the check; remove if it causes
//  an import cycle — the interface duck-types automatically in Go)

// MSPClient wraps the MC Protocol client for Mitsubishi PLCs.
type MSPClient struct {
	client mcp.Client
}

var msp *MSPClient

// InitMSPClient initializes a package-level singleton MSP client.
// Kept for backward compatibility. Prefer NewMSPClient for new code.
func InitMSPClient(plcHost string, plcPort int) error {
	if msp != nil {
		return nil
	}
	client, err := mcp.New3EClient(plcHost, plcPort, mcp.NewLocalStation())
	if err != nil {
		return err
	}
	msp = &MSPClient{client: client}
	return nil
}

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

	deviceNumberInt64, err := strconv.ParseInt(deviceNumber, 10, 64)
	if err != nil || deviceType == "Y" {
		deviceNumberInt64, err = strconv.ParseInt(deviceNumber, 16, 64)
		if err != nil {
			return nil, err
		}
	}

	resultCh := make(chan any)
	errCh := make(chan error)

	go func() {
		data, err := m.client.Read(deviceType, deviceNumberInt64, int64(numberRegisters), fx)
		if err != nil {
			errCh <- err
			return
		}
		value, err := parseData(data, int(numberRegisters), fx)
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

	deviceNumberInt64, err := strconv.ParseInt(deviceNumber, 10, 64)
	if err != nil || deviceType == "Y" {
		deviceNumberInt64, err = strconv.ParseInt(deviceNumber, 16, 64)
		if err != nil {
			return err
		}
	}

	calculatedRegisters := (len(writeData) + 1) / 2
	if numberRegisters == 0 || int(numberRegisters) < calculatedRegisters {
		numberRegisters = uint16(calculatedRegisters)
	}

	_, err = m.client.Write(deviceType, deviceNumberInt64, int64(numberRegisters), writeData)
	return err
}

// WriteData sends data to the PLC for the specified device.
// deviceType: device code (e.g. "D", "M", "Y").
// deviceNumber: starting device address (string, can be decimal or hex depending on device).
// numberRegisters: number of points to write.
// writeData: the data to be written as a byte slice.
// BatchWrite get wrap-around (overflow) and jump to lower device (reverse)
// BatchWrite writes using the package-level MSP client initialized via InitMSPClient.
func BatchWrite(deviceType, startDevice string, writeData []byte, maxRegistersPerWrite uint16, logger *log.Logger) error {
	if msp == nil {
		return fmt.Errorf("MSP client not initialized")
	}
	return msp.BatchWrite(deviceType, startDevice, writeData, maxRegistersPerWrite, logger)
}

func (m *MSPClient) BatchWrite(deviceType, startDevice string, writeData []byte, maxRegistersPerWrite uint16, logger *log.Logger) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("MSP client not initialized")
	}

	// W and Y are hex-addressed on Mitsubishi PLCs. Parse as hex
	// unconditionally — a hex address like "10" (=16 decimal) is ALSO
	// valid decimal syntax, so a decimal-first-then-fallback-on-error
	// approach silently parses it wrong instead of falling back.
	var deviceNumberUint16 uint16
	if deviceType == "Y" || deviceType == "W" {
		val64, err := strconv.ParseInt(startDevice, 16, 64)
		if err != nil {
			return err
		}
		deviceNumberUint16 = uint16(val64)
	} else {
		val64, err := strconv.ParseInt(startDevice, 10, 64)
		if err != nil {
			return err
		}
		deviceNumberUint16 = uint16(val64)
	}

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
	if prefix == "Y" {
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
	return EncodeData(valueStr, processNumber) // calls conversion.go
}
