package shibaura

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// serve answers each Modbus request with respond(txID, req).
func serve(t *testing.T, respond func(tx uint16, req []byte) []byte) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				for {
					req := make([]byte, 12)
					if _, err := io.ReadFull(conn, req); err != nil {
						return
					}
					conn.Write(respond(binary.BigEndian.Uint16(req), req))
				}
			}(conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func adu(tx uint16, pdu ...byte) []byte {
	b := make([]byte, 7, 7+len(pdu))
	binary.BigEndian.PutUint16(b, tx)
	binary.BigEndian.PutUint16(b[4:], uint16(len(pdu)+1))
	b[6] = 1
	return append(b, pdu...)
}

func TestReadHolding_ReusesConnection(t *testing.T) {
	port := serve(t, func(tx uint16, _ []byte) []byte {
		return adu(tx, 0x03, 2, 0x12, 0x34)
	})
	c := NewClient("127.0.0.1", port, 1)
	defer c.Close()
	for i := 0; i < 3; i++ {
		v, err := c.ReadHolding(0, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0] != 0x1234 {
			t.Fatalf("got %v", v)
		}
	}
}

// A reply with fewer bytes than requested used to panic in rawToBits.
func TestReadCoils_ShortResponseIsError(t *testing.T) {
	port := serve(t, func(tx uint16, _ []byte) []byte {
		return adu(tx, 0x01, 1, 0xFF) // 1 byte for 16 coils
	})
	c := NewClient("127.0.0.1", port, 1)
	c.timeout = time.Second
	defer c.Close()
	if _, err := c.ReadCoils(0, 16); err == nil {
		t.Fatal("expected error for short response")
	}
}

func TestRawToBits_ClampsToData(t *testing.T) {
	if got := rawToBits([]byte{0x01}, 16); len(got) != 8 || !got[0] {
		t.Fatalf("got %v", got)
	}
}
