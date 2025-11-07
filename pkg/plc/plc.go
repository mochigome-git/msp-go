package plc

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"unicode"

	"github.com/mochigome-git/msp-go/pkg/mcp"
)

type mspClient struct {
	client mcp.Client
}

var msp *mspClient

// InitMSPClient initializes the MSP client for the specified PLC host and port.
func InitMSPClient(plcHost string, plcPort int) error {
	if msp != nil {
		return nil
	}
	// Connect to the PLC with MC protocol
	client, err := mcp.New3EClient(plcHost, plcPort, mcp.NewLocalStation())
	if err != nil {
		return err
	}
	msp = &mspClient{client: client}
	return nil
}

// ReadData reads data from the PLC for the specified device.
func ReadData(ctx context.Context, deviceType string, deviceNumber string, numberRegisters uint16, fx bool) (any, error) {
	if msp == nil {
		return nil, fmt.Errorf("MSP client not initialized")
	}

	// Create a channel to receive the result or context cancellation
	resultCh := make(chan any)
	errCh := make(chan error)

	deviceNumberInt64, err := strconv.ParseInt(deviceNumber, 10, 64)
	if err != nil || deviceType == "Y" {

		// Convert the offset string to an integer
		deviceNumberInt64, err = strconv.ParseInt(deviceNumber, 16, 64)
		if err != nil {
			return nil, err
		}

		//return nil, err
	}

	// Start a goroutine to perform the data reading
	go func() {
		// Read data from the PLC
		data, err := msp.client.Read(deviceType, deviceNumberInt64, int64(numberRegisters), fx)
		if err != nil {
			errCh <- err
			return
		}

		// Parse the data based on the number of registers and fx condition
		value, err := ParseData(data, int(numberRegisters), fx)
		if err != nil {
			errCh <- err
			return
		}

		// Send the result on the channel
		resultCh <- value
	}()

	select {
	case <-ctx.Done():
		// Context is canceled before the operation completes
		return nil, ctx.Err()
	case err := <-errCh:
		// Error occurred during the data reading operation
		return nil, err
	case value := <-resultCh:
		// Data reading operation completed successfully
		return value, nil
	}
}

// WriteData sends data to the PLC for the specified device.
// deviceType: device code (e.g. "D", "M", "Y").
// deviceNumber: starting device address (string, can be decimal or hex depending on device).
// numberRegisters: number of points to write.
// writeData: the data to be written as a byte slice.
func WriteData(deviceType string, deviceNumber string, writeData []byte, numberRegisters uint16) error {
	if msp == nil {
		return fmt.Errorf("MSP client not initialized")
	}

	// Parse device number (hex for Y type, decimal otherwise)
	deviceNumberInt64, err := strconv.ParseInt(deviceNumber, 10, 64)
	if err != nil || deviceType == "Y" {
		deviceNumberInt64, err = strconv.ParseInt(deviceNumber, 16, 64)
		if err != nil {
			return err
		}
	}

	// Ensure numPoints matches data length (2 bytes per register)
	calculatedRegisters := (len(writeData) + 1) / 2
	if numberRegisters == 0 || int(numberRegisters) < calculatedRegisters {
		numberRegisters = uint16(calculatedRegisters)
	}

	// Write to consecutive registers
	_, err = msp.client.Write(deviceType, deviceNumberInt64, int64(numberRegisters), writeData)
	return err
}

// WriteData sends data to the PLC for the specified device.
// deviceType: device code (e.g. "D", "M", "Y").
// deviceNumber: starting device address (string, can be decimal or hex depending on device).
// numberRegisters: number of points to write.
// writeData: the data to be written as a byte slice.
// BatchWrite get wrap-around (overflow) and jump to lower device (reverse)
func BatchWrite(deviceType string, startDevice string, writeData []byte, maxRegistersPerWrite uint16, logger *log.Logger) error {
	var deviceNumberUint16 uint16
	var err error

	// Parse device number (hex for "Y", decimal for others)
	if deviceType == "Y" {
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

	// Start from the highest address (startDevice + totalRegisters - 1)
	currentAddr := deviceNumberUint16 + uint16(totalRegisters-1)

	for written < totalRegisters {
		remaining := totalRegisters - written
		chunkSize := remaining
		if chunkSize > int(maxRegistersPerWrite) {
			chunkSize = int(maxRegistersPerWrite)
		}

		// Calculate chunk indices (still forward in data, but written backward to device)
		startIndex := written * 2
		endIndex := startIndex + chunkSize*2
		if endIndex > len(writeData) {
			endIndex = len(writeData)
		}

		chunk := writeData[startIndex:endIndex]

		// Write from currentAddr backward
		//logger.Printf("Writing to %s device number %d, chunk size %d, data % X\n", deviceType, currentAddr-uint16(written), chunkSize, chunk)

		_, err = msp.client.Write(deviceType, int64(currentAddr-uint16(written)), int64(chunkSize), chunk)
		if err != nil {
			logger.Printf("❌ Failed to write %d registers to device %s at address %d: %v", chunkSize, deviceType, currentAddr-uint16(written), err)
			return err
		}

		// Log successful write
		logger.Printf("✅ Wrote %d registers to address %s%d: data=%X", chunkSize, deviceType, currentAddr-uint16(written), chunk)


		written += chunkSize
	}

	return nil
}

// IncrementDevice increments a device string like "W10" to "W11"
func IncrementDevice(device string, offset int64) (string, error) {
	// Separate the alphabetic prefix from the numeric suffix
	var prefix string
	var numStr string

	for i, r := range device {
		if unicode.IsDigit(r) || (r >= 'A' && r <= 'F') { // allow hex digits if needed
			prefix = device[:i]
			numStr = device[i:]
			break
		}
	}

	if prefix == "" {
		// If no prefix found, assume all numeric
		numStr = device
	}

	// Parse numeric part as int64
	// You might want to support hex parsing for 'Y' devices
	var base int = 10
	if prefix == "Y" {
		base = 16
	}
	num, err := strconv.ParseInt(numStr, base, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse device number part: %w", err)
	}

	// Add offset
	num += offset

	// Format back with prefix
	var newNumStr string
	if base == 16 {
		newNumStr = fmt.Sprintf("%X", num)
	} else {
		newNumStr = fmt.Sprintf("%d", num)
	}

	return prefix + newNumStr, nil
}
