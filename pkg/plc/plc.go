package plc

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	"msp-go/pkg/mcp"
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
func ReadData(ctx context.Context, deviceType string, deviceNumber string, numberRegisters uint16, fx bool) (interface{}, error) {
	if msp == nil {
		return nil, fmt.Errorf("MSP client not initialized")
	}

	// Create a channel to receive the result or context cancellation
	resultCh := make(chan interface{})
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

// ParseData parses the data based on the specified number of registers and fx condition
func ParseData(data []byte, numberRegisters int, fx bool) (any, error) {
	registerBinary, _ := mcp.NewParser().Do(data)
	if fx {
		registerBinary, _ = mcp.NewParser().DoFx(data)
	}
	data = registerBinary.Payload

	switch numberRegisters {
	case 1: // 16-bit unsigned
		var val uint16
		for i, b := range data {
			val |= uint16(b) << (8 * i)
		}
		return val, nil
	case 2: // 32-bit device (handles negative floats)
		var val uint32
		for i, b := range data {
			val |= uint32(b) << (8 * i)
		}
		floatValue := math.Float32frombits(val)
		// Format to 6 decimal places and take first 6 significant digits
		floatString := fmt.Sprintf("%.6f", floatValue)
		var builder strings.Builder
		digitsCount := 0
		for _, c := range floatString {
			if c == '-' || c == '.' {
				builder.WriteRune(c)
			} else if digitsCount < 6 {
				builder.WriteRune(c)
				digitsCount++
			}
		}
		return builder.String(), nil

	case 3: // 2-bit device
		var val uint8
		if len(data) >= 1 {
			val = uint8(data[0] & 0x01)
		}
		return val, nil

	case 4: // ASCII hex device
		var val uint16
		for i, b := range data {
			val |= uint16(b) << (8 * i)
		}
		text := fmt.Sprintf("%X", val)
		hexBytes, err := hex.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("error decoding hexadecimal string: %w", err)
		}
		return string(hexBytes), nil

	case 5: // 16-bit signed (handles negative values)
		var val int16
		for i, b := range data {
			val |= int16(b) << (8 * i)
		}
		return val, nil

	case 6: // 2-bit device for fx
		var val uint16
		for i, b := range data {
			val |= uint16(b/10) << (8 * i)
		}
		return val, nil

	default:
		return nil, fmt.Errorf("invalid number of registers: %d", numberRegisters)
	}
}
