package mcp

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
)

// fakePLC answers every request on one connection with respond(req).
func fakePLC(t *testing.T, reqLen int, respond func(req []byte) []byte) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			req := make([]byte, reqLen)
			if _, err := io.ReadFull(conn, req); err != nil {
				return
			}
			for _, b := range respond(req) { // one byte per segment
				if _, err := conn.Write([]byte{b}); err != nil {
					return
				}
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func frame3E(endCode uint16, data ...byte) []byte {
	resp := []byte{0xD0, 0x00, 0x00, 0xFF, 0xFF, 0x03, 0x00, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(resp[7:9], uint16(2+len(data)))
	binary.LittleEndian.PutUint16(resp[9:11], endCode)
	return append(resp, data...)
}

// A rejected word write used to look like success.
func TestClient3E_WriteReportsEndCode(t *testing.T) {
	codes := []uint16{0xC056, 0x0000}
	i := 0
	port := fakePLC(t, 23, func([]byte) []byte { // 21-byte header + 1 word
		c := codes[i%len(codes)]
		i++
		return frame3E(c)
	})
	c, _ := New3EClient("127.0.0.1", port, NewLocalStation())
	defer c.Close()

	_, err := c.Write("D", 100, 1, []byte{1, 0})
	if err == nil || !strings.Contains(err.Error(), "C056") {
		t.Fatalf("want end code error, got %v", err)
	}
	if _, err := c.Write("D", 100, 1, []byte{1, 0}); err != nil {
		t.Fatalf("successful write returned %v", err)
	}
}

// 1E responses must be read as whole frames even when they arrive in pieces.
func TestClient1E_ReadsFragmentedResponse(t *testing.T) {
	port := fakePLC(t, 12, func(req []byte) []byte {
		if req[0] == 0x00 { // bit read (FX01)
			return []byte{0x80, 0x00, 0x10}
		}
		return []byte{0x81, 0x00, 0x34, 0x12, 0x78, 0x56}
	})
	c, _ := New3EClient("127.0.0.1", port, NewLocalStation())
	defer c.Close()

	for i := 0; i < 2; i++ {
		resp, err := c.Read("D", 0, 2, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp) != 6 || resp[5] != 0x56 {
			t.Fatalf("word read %d: got % X", i, resp)
		}
	}
	resp, err := c.Read("M", 0, 6, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp) != 3 || resp[2] != 0x10 {
		t.Fatalf("bit read: got % X", resp)
	}
}
