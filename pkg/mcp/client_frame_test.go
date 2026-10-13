package mcp

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// A fake PLC that dribbles each response out in small TCP segments.
// The old client did a single conn.Read and would return a truncated
// frame here; block reads (up to ~1.9 KB) make this realistic.
func TestClient3E_ReadsFragmentedResponse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	const words = 960
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		for i := 0; i < 2; i++ { // serve two requests on one connection
			req := make([]byte, 21) // 3E batch read request is 21 bytes
			if _, err := io.ReadFull(conn, req); err != nil {
				return
			}
			n := int(binary.LittleEndian.Uint16(req[19:21]))
			resp := []byte{0xD0, 0x00, 0x00, 0xFF, 0xFF, 0x03, 0x00, 0, 0, 0, 0}
			binary.LittleEndian.PutUint16(resp[7:9], uint16(2+2*n))
			for w := 0; w < n; w++ {
				resp = binary.LittleEndian.AppendUint16(resp, uint16(w+i*1000))
			}
			for len(resp) > 0 { // 7-byte segments with small pauses
				k := min(7, len(resp))
				conn.Write(resp[:k])
				resp = resp[k:]
				time.Sleep(50 * time.Microsecond)
			}
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	c, err := New3EClient("127.0.0.1", addr.Port, NewLocalStation())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	for i := 0; i < 2; i++ {
		resp, err := c.Read("D", 0, words, false)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if len(resp) != 11+2*words {
			t.Fatalf("read %d: got %d bytes, want %d", i, len(resp), 11+2*words)
		}
		last := binary.LittleEndian.Uint16(resp[len(resp)-2:])
		if last != uint16(words-1+i*1000) {
			t.Fatalf("read %d: last word %d — frames got mixed up", i, last)
		}
	}
}

func TestClient3E_UnreachablePLCReturnsError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listening now

	c, _ := New3EClient("127.0.0.1", port, NewLocalStation())
	if _, err := c.Read("D", 0, 1, false); err == nil {
		t.Fatal("expected connect error (used to be log.Fatalf and kill the process)")
	}
}
