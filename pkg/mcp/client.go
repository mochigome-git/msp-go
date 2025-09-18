package mcp

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"sync"
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

	// Create connection if it's not already created
	if c.conn == nil {
		conn, err := net.DialTCP("tcp", nil, c.tcpAddr)
		if err != nil {
			log.Fatalf("❌ Failed to connect to PLC at %s: %v", c.tcpAddr.String(), err)
		}
		c.conn = conn
	}

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		// Close connection on error
		c.conn.Close()
		c.conn = nil
		return nil, err
	}

	// Receive message
	readBuff := make([]byte, 22+2*numPoints) // 22 is response header size. [sub header + network num + unit i/o num + unit station num + response length + response code]
	readLen, err := c.conn.Read(readBuff)
	if err != nil {
		// Close connection on error
		c.conn.Close()
		c.conn = nil
		return nil, err
	}

	//return readBuff[:readLen], nil
	// Process response in a separate goroutine
	response := readBuff[:readLen]
	resultChan := make(chan []byte)
	go func() {
		resultChan <- response
	}()

	return <-resultChan, nil

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
		conn, err := net.DialTCP("tcp", nil, c.tcpAddr)
		if err != nil {
			return nil, err
		}
		c.conn = conn
	}

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.conn.Close()
		c.conn = nil
		return nil, err
	}

	// Receive message
	readBuff := make([]byte, 22) // 22 = fixed response header size
	readLen, err := c.conn.Read(readBuff)
	if err != nil {
		c.conn.Close()
		c.conn = nil
		return nil, err
	}

	return readBuff[:readLen], nil
}
