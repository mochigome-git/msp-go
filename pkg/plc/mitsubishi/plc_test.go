package mitsubishi

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

func (m *mockClient) WriteBits(deviceType string, deviceNumber int64, bits []byte) ([]byte, error) {
	args := m.Called(deviceType, deviceNumber, bits)
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
	mockMCP.On("Write", "W", int64(0x10), int64(expectedRegisters), data).Return([]byte{}, nil).Once()

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

	// Expected Write calls: forward from the start address. W is hex, so
	// "10" = 16 → chunks at W10, W14, W18 (hex) = 16, 20, 24.
	mockMCP.On("Write", "W", int64(16), int64(4), writeData[0:8]).Return([]byte{}, nil).Once()
	mockMCP.On("Write", "W", int64(20), int64(4), writeData[8:16]).Return([]byte{}, nil).Once()
	mockMCP.On("Write", "W", int64(24), int64(2), writeData[16:20]).Return([]byte{}, nil).Once()

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

// A single bit value on a bit device goes out as a bit-unit write, so only
// that point changes (not the 16-point word).
func TestWriteData_BitDeviceUsesBitWrite(t *testing.T) {
	data, err := EncodeData("1", 3)
	assert.NoError(t, err)

	mockMCP := new(mockClient)
	mockMCP.On("WriteBits", "M", int64(70), []byte{1}).Return([]byte{}, nil).Once()

	client := &MSPClient{client: mockMCP}
	assert.NoError(t, client.WriteData("M", "70", data, 1))
	mockMCP.AssertExpectations(t)
}

// X is hex-addressed, as in GX Works: X3E is device 62.
func TestParseDeviceNumber_XIsHex(t *testing.T) {
	for _, c := range []struct {
		typ, num string
		want     int64
	}{{"X", "3E", 62}, {"X", "76", 118}, {"B", "10", 16}, {"M", "70", 70}, {"D", "3026", 3026}} {
		got, err := parseDeviceNumber(c.typ, c.num)
		assert.NoError(t, err)
		assert.Equal(t, c.want, got, c.typ+c.num)
	}
}
