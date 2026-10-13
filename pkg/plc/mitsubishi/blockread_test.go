package mitsubishi

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

// fakePLC simulates PLC device memory and answers 3E word batch reads with
// real response frames, so both the per-device path (ReadData/parseData)
// and the block path (ReadBlock) run their full decode logic.
type fakePLC struct {
	words map[string][]uint16 // word devices: D, W
	bits  map[string][]bool   // bit devices:  M, X, Y, B ...
	reads int
	limit int64 // device range size; reads past it get end code C056
}

func newFakePLC(seed int64) *fakePLC {
	r := rand.New(rand.NewSource(seed))
	f := &fakePLC{words: map[string][]uint16{}, bits: map[string][]bool{}, limit: 8192}
	for _, d := range []string{"D", "W"} {
		w := make([]uint16, f.limit)
		for i := range w {
			w[i] = uint16(r.Intn(1 << 16))
		}
		f.words[d] = w
	}
	for _, d := range []string{"M", "X", "Y", "B", "L"} {
		b := make([]bool, f.limit)
		for i := range b {
			b[i] = r.Intn(2) == 1
		}
		f.bits[d] = b
	}
	return f
}

func frame(endCode uint16, payload []byte) []byte {
	resp := []byte{0xD0, 0x00, 0x00, 0xFF, 0xFF, 0x03, 0x00, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(resp[7:9], uint16(2+len(payload)))
	binary.LittleEndian.PutUint16(resp[9:11], endCode)
	return append(resp, payload...)
}

func (f *fakePLC) Read(dev string, offset, n int64, fx bool) ([]byte, error) {
	f.reads++
	if n > 960 {
		return frame(0xC051, nil), nil // too many points
	}
	payload := make([]byte, 2*n)
	if w, ok := f.words[dev]; ok {
		if offset+n > f.limit {
			return frame(0xC056, nil), nil
		}
		for i := int64(0); i < n; i++ {
			binary.LittleEndian.PutUint16(payload[2*i:], w[offset+i])
		}
		return frame(0, payload), nil
	}
	if b, ok := f.bits[dev]; ok {
		if offset+16*n > f.limit {
			return frame(0xC056, nil), nil
		}
		for i := int64(0); i < 16*n; i++ {
			if b[offset+i] {
				payload[i/8] |= 1 << (i % 8)
			}
		}
		return frame(0, payload), nil
	}
	return nil, fmt.Errorf("fake: unknown device %s", dev)
}

func (f *fakePLC) Write(string, int64, int64, []byte) ([]byte, error) { return nil, nil }
func (f *fakePLC) Close() error                                       { return nil }

func dev(t string, n string, typ uint16) PLC_Utils.Device {
	return PLC_Utils.Device{DeviceType: t, DeviceNumber: n, NumberRegisters: typ}
}

// readAllPlanned runs a plan and returns address → value, exactly as the
// service would enqueue it (singles via ReadData).
func readAllPlanned(t *testing.T, c *MSPClient, plan *BlockPlan) map[string]any {
	t.Helper()
	got := map[string]any{}
	for _, b := range plan.Blocks {
		vals, err := c.ReadBlock(context.Background(), b)
		if err != nil {
			t.Fatalf("ReadBlock %s%d x%d: %v", b.DeviceType, b.Start, b.Words, err)
		}
		for _, v := range vals {
			if v.Err != nil {
				t.Fatalf("decode %s%s: %v", v.Device.DeviceType, v.Device.DeviceNumber, v.Err)
			}
			got[v.Device.DeviceType+v.Device.DeviceNumber+fmt.Sprintf("/%d", v.Device.NumberRegisters)] = v.Value
		}
	}
	for _, d := range plan.Singles {
		v, err := c.ReadData(context.Background(), d.DeviceType, d.DeviceNumber, d.NumberRegisters, false)
		if err != nil {
			t.Fatalf("single %s%s: %v", d.DeviceType, d.DeviceNumber, err)
		}
		got[d.DeviceType+d.DeviceNumber+fmt.Sprintf("/%d", d.NumberRegisters)] = v
	}
	return got
}

func readAllSingle(t *testing.T, c *MSPClient, devices []PLC_Utils.Device) map[string]any {
	t.Helper()
	want := map[string]any{}
	for _, d := range devices {
		v, err := c.ReadData(context.Background(), d.DeviceType, d.DeviceNumber, d.NumberRegisters, false)
		if err != nil {
			t.Fatalf("ReadData %s%s: %v", d.DeviceType, d.DeviceNumber, err)
		}
		want[d.DeviceType+d.DeviceNumber+fmt.Sprintf("/%d", d.NumberRegisters)] = v
	}
	return want
}

// The .env.local.example device list: 8 requests → 3.
func TestPlanBlocks_EnvExample(t *testing.T) {
	devices := []PLC_Utils.Device{
		dev("D", "0", 1), dev("D", "1", 1), dev("D", "2", 1), dev("D", "3", 1),
		dev("D", "650", 2), dev("D", "676", 2),
		dev("M", "24", 3), dev("M", "25", 3),
	}
	plan := PlanBlocks(devices, false, PlanOptions{})

	type shape struct {
		dt           string
		start, words int64
		members      int
	}
	var got []shape
	for _, b := range plan.Blocks {
		got = append(got, shape{b.DeviceType, b.Start, b.Words, len(b.Members)})
	}
	want := []shape{
		{"D", 0, 4, 4},    // D0-D3
		{"D", 650, 28, 2}, // D650-D677 (gap D4→D650 is 646 > 128, so split)
		{"M", 16, 1, 2},   // M24, M25 live in word M16-M31
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plan\n got  %+v\n want %+v", got, want)
	}
	if plan.Requests() != 3 || len(plan.Singles) != 0 {
		t.Fatalf("requests = %d singles = %d", plan.Requests(), len(plan.Singles))
	}
}

// Every value from the block path must equal the per-device path, for
// every data type, device kind and addressing quirk.
func TestReadBlock_MatchesSingleReads(t *testing.T) {
	devices := []PLC_Utils.Device{
		// word device, all block-able types, unsorted, with duplicates
		dev("D", "100", 1), dev("D", "101", 5), dev("D", "102", 2), dev("D", "104", 7),
		dev("D", "110", 4), dev("D", "120", 3), dev("D", "100", 2), dev("D", "7000", 1),
		dev("D", "50", 1), dev("D", "8000", 2),
		// W is hex-addressed: "1A" = 26, "100" = 256
		dev("W", "1A", 1), dev("W", "1B", 5), dev("W", "100", 2),
		// bit devices: single bits, unaligned starts, word-aligned word reads
		dev("M", "0", 3), dev("M", "5", 3), dev("M", "15", 3), dev("M", "16", 3),
		dev("M", "999", 3), dev("M", "1000", 3), dev("M", "64", 1), dev("M", "96", 2),
		dev("X", "3", 3), dev("Y", "1F", 3), dev("Y", "20", 3), dev("B", "100", 3),
		// must stay singles: unaligned word on bit device, fx-only type
		dev("M", "5", 1), dev("D", "200", 6),
	}

	for seed := int64(1); seed <= 20; seed++ {
		fake := newFakePLC(seed)
		c := &MSPClient{client: fake}

		want := readAllSingle(t, c, devices)
		singleReads := fake.reads

		fake.reads = 0
		plan := PlanBlocks(devices, false, PlanOptions{})
		got := readAllPlanned(t, c, plan)

		if !reflect.DeepEqual(got, want) {
			for k, w := range want {
				if !reflect.DeepEqual(got[k], w) {
					t.Errorf("seed %d %s: block=%#v single=%#v", seed, k, got[k], w)
				}
			}
			t.FailNow()
		}
		if seed == 1 {
			t.Logf("%d devices: %d single requests → %d block-mode requests", len(devices), singleReads, fake.reads)
		}
	}
}

// Random device lists across the whole address space, including dense
// clusters that must be split at the 960-word limit.
func TestReadBlock_Randomized(t *testing.T) {
	types := []uint16{1, 2, 3, 4, 5, 7}
	for seed := int64(1); seed <= 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		var devices []PLC_Utils.Device
		for i := 0; i < 20+r.Intn(150); i++ {
			typ := types[r.Intn(len(types))]
			switch r.Intn(4) {
			case 0, 1:
				devices = append(devices, dev("D", fmt.Sprint(r.Intn(8000)), typ))
			case 2:
				devices = append(devices, dev("W", fmt.Sprintf("%X", r.Intn(8000)), typ))
			default:
				a := r.Intn(8000)
				if typ != 3 {
					a -= a % 16
				}
				if a+16*int(spanWords[int(typ)]) > 8192 {
					continue
				}
				devices = append(devices, dev("M", fmt.Sprint(a), typ))
			}
		}
		opt := PlanOptions{MaxGapWords: int64(r.Intn(400))}

		fake := newFakePLC(seed)
		c := &MSPClient{client: fake}
		want := readAllSingle(t, c, devices)
		plan := PlanBlocks(devices, false, opt)
		for _, b := range plan.Blocks {
			if b.Words > DefaultMaxBlockWords {
				t.Fatalf("seed %d: block %s%d has %d words > 960", seed, b.DeviceType, b.Start, b.Words)
			}
			if b.BitDevice && b.Start%16 != 0 {
				t.Fatalf("seed %d: bit block %s%d not 16-aligned", seed, b.DeviceType, b.Start)
			}
		}
		got := readAllPlanned(t, c, plan)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d (gap %d): block values differ from single reads", seed, opt.MaxGapWords)
		}
	}
}

// A PLC error end code must fail the block, not publish garbage.
func TestReadBlock_EndCodeError(t *testing.T) {
	fake := newFakePLC(1)
	c := &MSPClient{client: fake}
	b := &Block{DeviceType: "D", Start: 8190, Words: 4,
		Members: []BlockMember{{Device: dev("D", "8190", 1), addr: 8190, typ: 1}}}
	if _, err := c.ReadBlock(context.Background(), b); err == nil {
		t.Fatal("expected end-code error for out-of-range block")
	}
	// and the single path now reports it too, instead of decoding error bytes
	if _, err := c.ReadData(context.Background(), "D", "8191", 2, false); err == nil {
		t.Fatal("expected end-code error from ReadData")
	}
}

func TestPlanBlocks_FXAllSingles(t *testing.T) {
	devices := []PLC_Utils.Device{dev("D", "0", 1), dev("D", "1", 1)}
	plan := PlanBlocks(devices, true, PlanOptions{})
	if len(plan.Blocks) != 0 || len(plan.Singles) != 2 {
		t.Fatalf("fx plan: %d blocks %d singles", len(plan.Blocks), len(plan.Singles))
	}
}
