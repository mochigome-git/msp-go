package mcp

import "testing"

func TestStation_BuildRRequest(t *testing.T) {
	station := NewLocalStation()
	request := station.BuildReadRequest("D", 300, 3)

	if request != "500000FFFF03000C001000010400002C0100A80300" {
		t.Fatalf("expected %v but actual is %v", "500000FFFF03000C001000010400002C0100A80300", request)
	}

	request2 := station.BuildReadRequest("D", 500, 50)
	if request2 != "500000FFFF03000C00100001040000F40100A83200" {
		t.Fatalf("expected %v but actual is %v", "500000FFFF03000C00100001040000F40100A83200", request2)
	}
}

// Bit-unit write of M70 = ON: sub-command 0001, 1 point, data nibble 0x10.
func TestStation_BuildBitWriteRequest(t *testing.T) {
	got := NewLocalStation().BuildBitWriteRequest("M", 70, []byte{1})
	want := "500000FFFF03000D00" + "1000" + "0114" + "0100" + "460000" + "90" + "0100" + "10"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	// 3 points 1,0,1 → 0x10 0x10
	got = NewLocalStation().BuildBitWriteRequest("M", 0, []byte{1, 0, 1})
	if got[len(got)-4:] != "1010" {
		t.Fatalf("packing wrong: %s", got)
	}
}
