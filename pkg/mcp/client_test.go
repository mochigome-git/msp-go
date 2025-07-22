package mcp

import (
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

var (
	testPLCHost string
	testPLCPort int
)

// Set the Actual PLC IP
// export PLC_TEST_HOST=192.168.0.100
// export PLC_TEST_PORT=5000

func init() {
	testPLCHost = strings.TrimSpace(os.Getenv("PLC_TEST_HOST"))
	if testPLCHost == "" {
		testPLCHost = "" // will trigger t.Skip later
	}
	if p := strings.TrimSpace(os.Getenv("PLC_TEST_PORT")); p != "" {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			testPLCPort = port
		}
	}
}

func TestClient3E_Read(t *testing.T) {

	// running only when there is and plc that can be accepted mc protocol
	if strings.TrimSpace(testPLCHost) == "" {
		t.Skip("environment variable PLC_TEST_HOST is not set or empty")
	}
	if testPLCPort <= 0 {
		t.Skip("environment variable PLC_TEST_PORT is not set or invalid")
	}

	client, err := New3EClient(testPLCHost, testPLCPort, NewLocalStation())
	if err != nil {
		t.Fatalf("PLC does not exists? %v", err)
	}

	// 1 device
	resp1, err := client.Read("D", 100, 1, false)
	if err != nil {
		t.Fatalf("unexpected mcp read err: %v", err)
	}

	if len(resp1) != 13 {
		t.Fatalf("expected %v but actual is %v", 13, len(resp1))
	}
	if hex.EncodeToString(resp1) != strings.ReplaceAll("d000 00 ff ff03 0004 0000 0000 00", " ", "") {
		t.Fatalf("expected %v but actual is %v", "d00000ffff0300040000000000", hex.EncodeToString(resp1))
	}

	// 3 device
	resp2, err := client.Read("D", 100, 5, false)
	if err != nil {
		t.Fatalf("unexpected mcp read err: %v", err)
	}

	if len(resp2) != 21 {
		t.Fatalf("expected %v but actual is %v", 21, len(resp2))
	}

	if hex.EncodeToString(resp2) != strings.ReplaceAll("d000 00 ff ff03 000c 0000 0000 000000000000000000", " ", "") {
		t.Fatalf("expected %v but actual is %v", "d00000ffff03000c00000000000000000000000000", hex.EncodeToString(resp2))
	}

}

func TestClient3E_BitRead(t *testing.T) {
	// running only when there is and plc that can be accepted mc protocol
	if testPLCHost == "" {
		t.Skip("environment variable PLC_TEST_HOST is not set")
	}
	if testPLCPort == 0 {
		t.Skip("environment variable PLC_TEST_PORT is not set")
	}

	client, err := New3EClient(testPLCHost, testPLCPort, NewLocalStation())
	if err != nil {
		t.Fatalf("PLC does not exists? %v", err)
	}

	// 1 device
	resp1, err := client.Read("B", 100, 1, false)
	if err != nil {
		t.Fatalf("unexpected mcp read err: %v", err)
	}

	if len(resp1) != 12 {
		t.Fatalf("expected %v but actual is %v", 12, len(resp1))
	}
	if hex.EncodeToString(resp1) != strings.ReplaceAll("d000 00 ff ff03 0003 0000 0000", " ", "") {
		t.Fatalf("expected %v but actual is %v", "d00000ffff03000300000000", hex.EncodeToString(resp1))
	}

	// 3 device
	resp2, err := client.Read("B", 0, 5, false)
	if err != nil {
		t.Fatalf("unexpected mcp read err: %v", err)
	}

	if len(resp2) != 14 {
		t.Fatalf("expected %v but actual is %v", 14, len(resp2))
	}

	if hex.EncodeToString(resp2) != strings.ReplaceAll("d000 00 ff ff03 0005 0000 0000 0000", " ", "") {
		t.Fatalf("expected %v but actual is %v", "d00000ffff030005000000000000", hex.EncodeToString(resp2))
	}

	// numpoints 5 and 6 will return same responce length
	resp3, err := client.Read("B", 0, 6, false)
	if err != nil {
		t.Fatalf("unexpected mcp read err: %v", err)
	}

	if len(resp3) != 14 {
		t.Fatalf("expected %v but actual is %v", 14, len(resp3))
	}

	if hex.EncodeToString(resp2) != strings.ReplaceAll("d000 00 ff ff03 0005 0000 0000 0000", " ", "") {
		t.Fatalf("expected %v but actual is %v", "d00000ffff030005000000000000", hex.EncodeToString(resp3))
	}
}

//func TestClient3E_Write(t *testing.T) {
//	// running only when there is and plc that can be accepted mc protocol
//	if testPLCHost == "" {
//		t.Skip("environment variable PLC_TEST_HOST is not set")
//	}
//	if testPLCPort == 0 {
//		t.Skip("environment variable PLC_TEST_PORT is not set")
//	}
//
//	client, err := New3EClient(testPLCHost, testPLCPort, NewLocalStation())
//	if err != nil {
//		t.Fatalf("PLC does not exists? %v", err)
//	}
//
//	_, err = client.Write("D", 100, 4, []byte("test"))
//	if err != nil {
//		t.Fatalf("unexpected mcp write err: %v", err)
//	}
//}
//
//func TestClient3E_Ping(t *testing.T) {
//	// running only when there is and plc that can be accepted mc protocol
//	if testPLCHost == "" {
//		t.Skip("environment variable PLC_TEST_HOST is not set")
//	}
//	if testPLCPort == 0 {
//		t.Skip("environment variable PLC_TEST_PORT is not set")
//	}
//
//	client, err := New3EClient(testPLCHost, testPLCPort, NewLocalStation())
//	if err != nil {
//		t.Fatalf("PLC does not exists? %v", err)
//	}
//
//}
