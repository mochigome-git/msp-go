// Package shibaura provides a TEST Modbus TCP client for Shibaura Machine
// (formerly Toshiba Machine) injection molding PLCs (TCZPW1A / TCZMAIN).
//
// # STATUS: EXPERIMENTAL — DO NOT USE IN PRODUCTION
//
// The Shibaura TCZPW1A protocol is undocumented. This probes the PLC using
// standard Modbus TCP as the most likely candidate. Results are unverified
// until confirmed against the actual machine.
//
// Safe usage order:
//  1. Call ScanPorts() to find which port responds
//  2. Call ReadHolding(0, 50) and correlate values against the HMI screen
//  3. Build your register map before trusting any address
//  4. WriteData is disabled until the map is confirmed
package shibaura

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// ProbePorts lists the TCP ports to scan on a Shibaura PLC.
// 502   = Modbus TCP standard
// 2001  = Toshiba S/T-series alternate
// 9094  = Toshiba T2/T3 ASCII computer link
// 10001 = serial-over-LAN adapters
// 44818 = EtherNet/IP
var ProbePorts = []int{502, 2001, 9094, 10001, 44818}

// Client is a TEST Modbus TCP probe for Shibaura PLCs.
// It satisfies pkg/plc.PLCClient so it can be dropped in wherever
// a PLCClient is expected.
type Client struct {
	host    string
	port    int
	unitID  byte
	timeout time.Duration

	mu   sync.Mutex // serializes requests on conn
	conn net.Conn   // reused across requests; dropped on any error
	txID uint16
}

// NewClient creates a Shibaura test client.
//
//	host:   PLC IP (e.g. "192.168.1.10")
//	port:   use ScanPorts() first if unsure
//	unitID: Modbus unit/slave address — try 1 first
func NewClient(host string, port int, unitID byte) *Client {
	return &Client{
		host:    host,
		port:    port,
		unitID:  unitID,
		timeout: 3 * time.Second,
	}
}

// ScanPorts checks which ports in ProbePorts have an open TCP socket.
// Run this before anything else.
//
// Example: map[502:false 2001:false 9094:true 10001:false 44818:false]
// → use port 9094.
func (c *Client) ScanPorts() map[int]bool {
	results := make(map[int]bool)
	for _, p := range ProbePorts {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.host, p), 1*time.Second)
		if err == nil {
			conn.Close()
			results[p] = true
		} else {
			results[p] = false
		}
	}
	return results
}

// ReadHolding reads count holding registers (FC03) starting at addr.
// Use this for initial discovery — dump registers 0–50 while watching
// the HMI to build your register map.
func (c *Client) ReadHolding(startAddr, count uint16) ([]uint16, error) {
	raw, err := c.modbusRequest(0x03, startAddr, count)
	if err != nil {
		return nil, err
	}
	return rawToUint16(raw), nil
}

// ReadInput reads count input registers (FC04) starting at addr.
// Some PLCs put read-only process values here instead of holding registers.
func (c *Client) ReadInput(startAddr, count uint16) ([]uint16, error) {
	raw, err := c.modbusRequest(0x04, startAddr, count)
	if err != nil {
		return nil, err
	}
	return rawToUint16(raw), nil
}

// ReadCoils reads count coil bits (FC01) — discrete boolean outputs.
func (c *Client) ReadCoils(startAddr, count uint16) ([]bool, error) {
	raw, err := c.modbusRequest(0x01, startAddr, count)
	if err != nil {
		return nil, err
	}
	return rawToBits(raw, count), nil
}

// ReadDiscreteInputs reads count discrete input bits (FC02).
func (c *Client) ReadDiscreteInputs(startAddr, count uint16) ([]bool, error) {
	raw, err := c.modbusRequest(0x02, startAddr, count)
	if err != nil {
		return nil, err
	}
	return rawToBits(raw, count), nil
}

// ── PLCClient interface ───────────────────────────────────────────────────────

// ReadData satisfies pkg/plc.PLCClient.
//
// Device type → Modbus function code mapping (best guess, unverified):
//
//	"X", "I"       → FC02 Discrete Inputs  (physical inputs on Toshiba)
//	"Y", "O"       → FC01 Coils            (outputs)
//	everything else → FC03 Holding Registers (D, W, R, H, G data registers)
//
// deviceNumber: decimal string address (e.g. "0", "100")
// numberRegisters: how many registers/bits to read. NOTE: unlike the
// Mitsubishi driver, where the third DEVICES field is a data-type code,
// here it is a plain count and the value is a []uint16 or []bool.
// fx is accepted for interface compatibility but ignored.
func (c *Client) ReadData(
	ctx context.Context,
	deviceType string,
	deviceNumber string,
	numberRegisters uint16,
	fx bool,
) (any, error) {
	var startAddr uint16
	if _, err := fmt.Sscanf(deviceNumber, "%d", &startAddr); err != nil {
		return nil, fmt.Errorf("shibaura: invalid deviceNumber %q: %w", deviceNumber, err)
	}

	switch deviceType {
	case "X", "I":
		return c.ReadDiscreteInputs(startAddr, numberRegisters)
	case "Y", "O":
		return c.ReadCoils(startAddr, numberRegisters)
	default:
		return c.ReadHolding(startAddr, numberRegisters)
	}
}

// WriteData is disabled until the Shibaura register map is confirmed.
// Writing unknown registers to a live injection molding machine is dangerous.
func (c *Client) WriteData(
	deviceType string,
	deviceNumber string,
	writeData []byte,
	numberRegisters uint16,
) error {
	return fmt.Errorf(
		"shibaura: WriteData is disabled — confirm register map against HMI before enabling writes",
	)
}

// BatchWrite is disabled for the same safety reason as WriteData.
func (c *Client) BatchWrite(
	deviceType string,
	startDevice string,
	writeData []byte,
	maxRegistersPerWrite uint16,
	logger *log.Logger,
) error {
	return fmt.Errorf("shibaura: BatchWrite disabled — see WriteData")
}

// ── internal Modbus TCP framing ───────────────────────────────────────────────

func (c *Client) modbusRequest(fc byte, addr, count uint16) ([]byte, error) {
	if count == 0 {
		return nil, fmt.Errorf("shibaura: count must be > 0")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", c.host, c.port), c.timeout)
		if err != nil {
			return nil, fmt.Errorf("shibaura: connect %s:%d: %w", c.host, c.port, err)
		}
		c.conn = conn
	}
	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))

	c.txID++
	txID := c.txID

	// Modbus TCP ADU: MBAP header (7 bytes) + PDU (5 bytes)
	req := make([]byte, 12)
	binary.BigEndian.PutUint16(req[0:], txID) // transaction ID
	req[2], req[3] = 0x00, 0x00               // protocol ID (always 0 for Modbus)
	req[4], req[5] = 0x00, 0x06               // remaining length
	req[6] = c.unitID
	req[7] = fc
	binary.BigEndian.PutUint16(req[8:], addr)
	binary.BigEndian.PutUint16(req[10:], count)

	payload, err := c.roundTrip(req, txID)
	if err != nil {
		c.dropConn()
		return nil, err
	}

	// payload[0] = echoed FC (or FC|0x80 on error)
	// payload[1] = byte count (normal) or exception code (error)
	// payload[2:] = register data
	if payload[0]&0x80 != 0 {
		return nil, fmt.Errorf("shibaura: modbus exception fc=0x%02X code=%d", payload[0], payload[1])
	}
	if payload[0] != fc {
		c.dropConn()
		return nil, fmt.Errorf("shibaura: response fc=0x%02X, want 0x%02X", payload[0], fc)
	}

	want := int(count) * 2 // registers
	if fc == 0x01 || fc == 0x02 {
		want = (int(count) + 7) / 8 // bits
	}
	data := payload[2:]
	if int(payload[1]) != len(data) || len(data) < want {
		c.dropConn()
		return nil, fmt.Errorf("shibaura: short response: byte count %d, got %d bytes, want %d", payload[1], len(data), want)
	}
	return data, nil
}

func (c *Client) roundTrip(req []byte, txID uint16) ([]byte, error) {
	if _, err := c.conn.Write(req); err != nil {
		return nil, fmt.Errorf("shibaura: send: %w", err)
	}

	// Read MBAP header to get payload length
	header := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, fmt.Errorf("shibaura: read header: %w", err)
	}
	if got := binary.BigEndian.Uint16(header[0:2]); got != txID {
		return nil, fmt.Errorf("shibaura: transaction id %d, want %d", got, txID)
	}

	// length field includes unit ID (already in header[6]), subtract 1
	payloadLen := int(binary.BigEndian.Uint16(header[4:6])) - 1
	if payloadLen < 2 {
		return nil, fmt.Errorf("shibaura: response too short (payloadLen=%d)", payloadLen)
	}

	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(c.conn, payload); err != nil {
		return nil, fmt.Errorf("shibaura: read payload: %w", err)
	}
	return payload, nil
}

func (c *Client) dropConn() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Close closes the connection to the PLC.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropConn()
	return nil
}

func rawToUint16(raw []byte) []uint16 {
	out := make([]uint16, len(raw)/2)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(raw[i*2:])
	}
	return out
}

func rawToBits(raw []byte, count uint16) []bool {
	if max := uint16(len(raw)) * 8; count > max {
		count = max
	}
	out := make([]bool, count)
	for i := uint16(0); i < count; i++ {
		out[i] = (raw[i/8]>>(i%8))&1 == 1
	}
	return out
}

func (c *Client) EncodeData(valueStr string, processNumber int) ([]byte, error) {
	// TODO: implement when Shibaura register map is confirmed
	// For now writes are disabled anyway, so this won't be called
	return nil, fmt.Errorf("shibaura: EncodeData not implemented")
}
