package plc

import (
	"fmt"
	"testing"

	"github.com/mochigome-git/msp-go/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockClient mocks mcp.Client interface used by plc.mspClient
type mockClient struct {
	mock.Mock
}

func (m *mockClient) Write(deviceType string, deviceNumber int64, numPoints int64, data []byte) ([]byte, error) {
	fmt.Printf("Write called: deviceType=%s, deviceNumber=%d, numPoints=%d, data=% X\n", deviceType, deviceNumber, numPoints, data)
	args := m.Called(deviceType, deviceNumber, numPoints, data)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *mockClient) Read(deviceType string, deviceNumber int64, numPoints int64, fx bool) ([]byte, error) {
	args := m.Called(deviceType, deviceNumber, numPoints, fx)
	return args.Get(0).([]byte), args.Error(1)
}

func (m *mockClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestWriteData_ASCIIRegisters(t *testing.T) {
	value := "Initial failed"
	data, err := EncodeData(value, 4)
	assert.NoError(t, err)

	mockMCP := new(mockClient)

	expectedRegisters := uint16((len(data) + 1) / 2)
	mockMCP.On("Write", "W", int64(10), int64(expectedRegisters), data).
		Return([]byte{}, nil).
		Once()

	SetMspClientMock(mockMCP)

	err = WriteData("W", "10", data, 0)
	assert.NoError(t, err)

	calls := mockMCP.Calls
	for i, call := range calls {
		fmt.Printf("Call %d: method=%s, args=%v\n", i, call.Method, call.Arguments)
	}

	mockMCP.AssertExpectations(t)
}

// Test helper to inject a mock client
func SetMspClientMock(client mcp.Client) {
	msp = &mspClient{client: client}
}

func TestBatchWrite(t *testing.T) {
	mockMCP := new(mockClient)

	// Create dummy data 10 registers (20 bytes)
	writeData := make([]byte, 20)
	for i := 0; i < 20; i++ {
		writeData[i] = byte(i + 1)
	}

	// maxRegistersPerWrite = 4, so batch write 4 registers at a time (8 bytes)
	maxRegisters := uint16(4)

	// Expect Write calls for 3 batches:
	// Batch 1: deviceNumber 10, 4 registers, bytes 0-7
	mockMCP.On("Write", "W", int64(10), int64(4), writeData[0:8]).
		Return([]byte{}, nil).
		Once()

	// Batch 2: deviceNumber 14, 4 registers, bytes 8-15
	mockMCP.On("Write", "W", int64(14), int64(4), writeData[8:16]).
		Return([]byte{}, nil).
		Once()

	// Batch 3: deviceNumber 18, 2 registers, bytes 16-19 (last chunk)
	mockMCP.On("Write", "W", int64(18), int64(2), writeData[16:20]).
		Return([]byte{}, nil).
		Once()

	SetMspClientMock(mockMCP)

	err := BatchWrite("W", "10", writeData, maxRegisters, nil)
	assert.NoError(t, err)

	mockMCP.AssertExpectations(t)
}
