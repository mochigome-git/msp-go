package plc

import (
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockClient mocks mcp.Client interface used by MSPClient
type mockClient struct {
	mock.Mock
}

var parseDataFunc = parseData

func (m *mockClient) Write(deviceType string, deviceNumber int64, numPoints int64, data []byte) ([]byte, error) {
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

// ------------------- Test WriteData -------------------

func TestWriteData_ASCIIRegisters(t *testing.T) {
	value := "Initial failed"
	data, err := EncodeData(value, 4)
	assert.NoError(t, err)

	mockMCP := new(mockClient)
	expectedRegisters := uint16((len(data) + 1) / 2)

	// Expect Write call
	mockMCP.On("Write", "W", int64(10), int64(expectedRegisters), data).Return([]byte{}, nil).Once()

	// Create MSPClient instance with mock
	client := &MSPClient{client: mockMCP}

	// Call WriteData using instance
	err = client.WriteData("W", "10", data, 0)
	assert.NoError(t, err)

	mockMCP.AssertExpectations(t)
}

// ------------------- Test BatchWrite -------------------

func TestBatchWrite(t *testing.T) {
	mockMCP := new(mockClient)

	writeData := make([]byte, 20) // 10 registers (2 bytes each)
	for i := range writeData {
		writeData[i] = byte(i + 1)
	}

	maxRegisters := uint16(4) // batch size 4 registers

	// Expected Write calls (address calculated for backward batch)
	mockMCP.On("Write", "W", int64(19), int64(4), writeData[0:8]).Return([]byte{}, nil).Once()
	mockMCP.On("Write", "W", int64(15), int64(4), writeData[8:16]).Return([]byte{}, nil).Once()
	mockMCP.On("Write", "W", int64(11), int64(2), writeData[16:20]).Return([]byte{}, nil).Once()

	// Inject mock into MSPClient
	client := &MSPClient{client: mockMCP}

	// Provide a logger so BatchWrite can log
	logger := log.Default()

	// Call BatchWrite
	err := client.BatchWrite("W", "10", writeData, maxRegisters, logger)
	assert.NoError(t, err)

	mockMCP.AssertExpectations(t)
}

// ------------------- Test ReadData -------------------
