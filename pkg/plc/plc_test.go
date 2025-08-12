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
