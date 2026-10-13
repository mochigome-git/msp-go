package mcp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Client interface {
	Read(deviceName string, offset, numPoints int64, fx bool) ([]byte, error)
	Write(deviceName string, offset, numPoints int64, writeData []byte) ([]byte, error)
	Close() error
}

type client3E struct {
	// PLC address
	tcpAddr *net.TCPAddr
	// PLC station
	stn *station
	// TCP connection
	conn net.Conn
	// Mutex to synchronize access to conn
	mu sync.Mutex

	fx bool
}

func New3EClient(host string, port int, stn *station) (Client, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%v:%v", host, port))
	if err != nil {
		return nil, err
	}
	return &client3E{tcpAddr: tcpAddr, stn: stn}, nil
}

func (c *client3E) Read(deviceName string, offset int64, numPoints int64, fx bool) ([]byte, error) {
	requestStr := c.stn.BuildReadRequest(deviceName, offset, numPoints)
	if fx {
		requestStr = c.stn.BuildReadRequestFx(deviceName, offset, numPoints)
	}

	// TODO binary protocol
	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Create connection if it's not already created.
	// Was log.Fatalf: one unreachable PLC killed the whole process,
	// including reads from the other PLC. Now the caller sees an error
	// and the next cycle retries the dial.
	if c.conn == nil {
		conn, err := net.DialTimeout("tcp", c.tcpAddr.String(), ioTimeout)
		if err != nil {
			return nil, fmt.Errorf("connect PLC %s: %w", c.tcpAddr.String(), err)
		}
		c.conn = conn
	}
	_ = c.conn.SetDeadline(time.Now().Add(ioTimeout))

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.dropConn()
		return nil, err
	}

	var response []byte
	if fx {
		// 1E frame: no length field in the header; keep the original
		// single-read behaviour.
		readBuff := make([]byte, 22+2*numPoints)
		n, rerr := c.conn.Read(readBuff)
		response, err = readBuff[:n], rerr
	} else {
		response, err = readResponse3E(c.conn)
	}
	if err != nil {
		c.dropConn()
		return nil, err
	}
	return response, nil
}

// ioTimeout bounds every dial/request/response. Without it a PLC that
// stops answering blocks the read loop forever while holding c.mu.
const ioTimeout = 5 * time.Second

// readResponse3E reads exactly one 3E-frame binary response.
//
// TCP is a stream: a single conn.Read can return part of a response
// (likely for block reads of hundreds of words) or, after a timeout, the
// tail of a previous one. The header carries the data length, so read
// the 9-byte header, then exactly that many bytes.
//
// Layout: subheader(2) network(1) pc(1) unitIO(2) station(1) dataLen(2 LE)
// then dataLen bytes = end code(2) + data.
func readResponse3E(conn net.Conn) ([]byte, error) {
	header := make([]byte, 9)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read response header: %w", err)
	}
	dataLen := int(binary.LittleEndian.Uint16(header[7:9]))
	if dataLen < 2 {
		return nil, fmt.Errorf("invalid response data length %d", dataLen)
	}
	resp := make([]byte, 9+dataLen)
	copy(resp, header)
	if _, err := io.ReadFull(conn, resp[9:]); err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return resp, nil
}

func (c *client3E) dropConn() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *client3E) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Write is send write command to remote plc by mc protocol
// deviceName is device code name like 'D' register.
// offset is device offset addr.
// writeData is data to write.
// numPoints is number of write device points.
// writeData is the data to be written. If writeData is larger than 2*numPoints bytes,
// data larger than 2*numPoints bytes is ignored.
func (c *client3E) Write(deviceName string, offset, numPoints int64, writeData []byte) ([]byte, error) {
	requestStr := c.stn.BuildWriteRequest(deviceName, offset, numPoints, writeData)
	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Create connection if it's not already created
	if c.conn == nil {
		conn, err := net.DialTimeout("tcp", c.tcpAddr.String(), ioTimeout)
		if err != nil {
			return nil, err
		}
		c.conn = conn
	}
	_ = c.conn.SetDeadline(time.Now().Add(ioTimeout))

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.dropConn()
		return nil, err
	}

	// Receive message (full frame — reads and writes share this conn, so
	// a partially-consumed response would corrupt the next read)
	resp, err := readResponse3E(c.conn)
	if err != nil {
		c.dropConn()
		return nil, err
	}
	return resp, nil
}
