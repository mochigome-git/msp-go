package plc

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

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
		text := fmt.Sprintf("%04X", val)
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

// WriteData sends data to the PLC for the specified device.
// deviceType: device code (e.g. "D", "M", "Y").
// deviceNumber: starting device address (string, can be decimal or hex depending on device).
// numberRegisters: number of points to write.
// writeData: the data to be written as a byte slice.
func WriteData(deviceType string, deviceNumber string, writeData []byte) error {
	if msp == nil {
		return fmt.Errorf("MSP client not initialized")
	}

	// Each PLC register = 2 bytes
	numberRegisters := (len(writeData) + 1) / 2

	// Parse device number (hex for Y type, decimal otherwise)
	deviceNumberInt64, err := strconv.ParseInt(deviceNumber, 10, 64)
	if err != nil || deviceType == "Y" {
		deviceNumberInt64, err = strconv.ParseInt(deviceNumber, 16, 64)
		if err != nil {
			return err
		}
	}

	// Write to consecutive registers
	_, err = msp.client.Write(deviceType, deviceNumberInt64, int64(numberRegisters), writeData)
	return err
}

// EncodeData encodes a value string into a byte slice for PLC writing,
// based on the expected number of registers and data type.
func EncodeData(valueStr string, numberRegisters int) ([]byte, error) {
	valueStr = strings.TrimSpace(valueStr)

	switch numberRegisters {
	case 1: // 16-bit unsigned or signed int (we use uint16 here)
		// Parse as int
		val, err := strconv.ParseUint(valueStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse uint16: %w", err)
		}
		data := make([]byte, 2)
		data[0] = byte(val & 0xFF)
		data[1] = byte((val >> 8) & 0xFF)
		return data, nil

	case 2: // 32-bit float (like ParseData case 2)
		fval, err := strconv.ParseFloat(valueStr, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float32: %w", err)
		}
		bits := math.Float32bits(float32(fval))
		data := make([]byte, 4)
		for i := range data {
			data[i] = byte(bits >> (8 * i) & 0xFF)
		}
		return data, nil

	case 3: // 2-bit device (single bit)
		// Accept "true"/"false" or "1"/"0"
		val := byte(0x00)
		switch strings.ToLower(valueStr) {
		case "true", "1", "on":
			val = 0x01
		case "false", "0", "off":
			val = 0x00
		default:
			return nil, fmt.Errorf("invalid bit value: %s", valueStr)
		}
		return []byte{val}, nil

	case 4: // ASCII hex device
		asciiBytes := []byte(valueStr)

		neededBytes := numberRegisters * 2
		if len(asciiBytes) < neededBytes {
			padded := make([]byte, neededBytes)
			copy(padded, asciiBytes)
			asciiBytes = padded
		} else if len(asciiBytes) > neededBytes {
			asciiBytes = asciiBytes[:neededBytes]
		}

		// Rearrange into big-endian words for PLC
		data := make([]byte, neededBytes)
		for i := 0; i < neededBytes; i += 2 {
			if i+1 < len(asciiBytes) {
				data[i] = asciiBytes[i]     // high byte
				data[i+1] = asciiBytes[i+1] // low byte
			} else {
				data[i] = asciiBytes[i]
				data[i+1] = 0
			}
		}

		return data, nil

	case 5: // 16-bit signed int
		val, err := strconv.ParseInt(valueStr, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("failed to parse int16: %w", err)
		}
		data := make([]byte, 2)
		data[0] = byte(val & 0xFF)
		data[1] = byte((val >> 8) & 0xFF)
		return data, nil

	case 6: // 2-bit device for fx (special case, treat as uint16 divided by 10)
		// Assume the value is an integer or float representing the device value * 10
		fval, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse float for fx device: %w", err)
		}
		ival := uint16(fval * 10)
		data := make([]byte, 2)
		data[0] = byte(ival & 0xFF)
		data[1] = byte((ival >> 8) & 0xFF)
		return data, nil

	default:
		return nil, fmt.Errorf("unsupported number of registers: %d", numberRegisters)
	}
}
