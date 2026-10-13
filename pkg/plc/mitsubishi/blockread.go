package mitsubishi

// Block reads: turn a flat device list into a few batch-read requests.
//
// Why: the PLC answers one MC-protocol request per END processing, so every
// request costs roughly one scan (~30-40 ms) no matter how many words it
// returns. 140 single-device requests = 140 scans per cycle. Grouping nearby
// addresses into one request of up to 960 words costs about the same as one
// single-device read.
//
// How the grouping works:
//
//   - Each configured device becomes a "member" with a span: where it starts
//     and how much of the device memory its data type consumes.
//   - Members are grouped by device code (D, W, M, ...) and sorted by start.
//   - A block is grown greedily while the next member starts within MaxGap
//     words of the block's current end and the block stays <= MaxWords.
//   - After the read, each member's value is cut out of the block payload
//     and decoded with the same decodeValue() the single-device path uses,
//     so values are identical to before.
//
// Bit devices (X, Y, M, L, F, V, B) are read in word units (16 points per
// word) exactly like the existing single reads do. The planner works in bit
// positions for those, and aligns every bit-device block start down to a
// multiple of 16, which the MC protocol always accepts for word access.
//
// What stays a single read (planned as Singles, read the old way):
//   - FX PLCs (1E frame, different command set)
//   - device codes the 3E builder doesn't know (anything not in deviceCodes)
//   - type code 6 (FX-only encoding) or unknown type codes
//   - a word-typed value on a bit device that doesn't start on a 16-point
//     boundary (e.g. M,5,1) — it would need bit shifting; rare in practice

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/mochigome-git/msp-go/pkg/mcp"
	PLC_Utils "github.com/mochigome-git/msp-go/pkg/utils"
)

const (
	// DefaultMaxBlockWords is the 3E-frame batch read limit (Q/L/iQ-R,
	// binary, word units).
	DefaultMaxBlockWords = 960
	// DefaultMaxGapWords: unused words we'll read to avoid splitting a
	// block. Extra words cost microseconds; an extra request costs a scan.
	DefaultMaxGapWords = 128
)

// PlanOptions tunes the planner. Zero values pick the defaults.
type PlanOptions struct {
	MaxBlockWords int64
	MaxGapWords   int64
}

func (o PlanOptions) withDefaults() PlanOptions {
	if o.MaxBlockWords <= 0 || o.MaxBlockWords > DefaultMaxBlockWords {
		o.MaxBlockWords = DefaultMaxBlockWords
	}
	if o.MaxGapWords < 0 {
		o.MaxGapWords = 0
	} else if o.MaxGapWords == 0 {
		o.MaxGapWords = DefaultMaxGapWords
	}
	return o
}

// Block is one batch-read request.
type Block struct {
	DeviceType string // "D", "M", ...
	Start      int64  // first device number read (bit devices: multiple of 16)
	Words      int64  // number of words requested
	BitDevice  bool
	Members    []BlockMember
}

// BlockMember is one configured device served by a block.
type BlockMember struct {
	Device PLC_Utils.Device
	addr   int64 // parsed device number
	typ    int   // data-type code (Device.NumberRegisters)
	start  int64 // in units: words for word devices, bits for bit devices
	end    int64 // exclusive, same units
}

// BlockPlan is the result of planning one PLC's device list.
type BlockPlan struct {
	Blocks  []*Block
	Singles []PLC_Utils.Device // read with the old per-device path
}

// Requests is the number of PLC round trips one cycle will take.
func (p *BlockPlan) Requests() int { return len(p.Blocks) + len(p.Singles) }

// BlockValue is one decoded member value from a block read.
type BlockValue struct {
	Device PLC_Utils.Device
	Value  any
	Err    error
}

var bitDevices = map[string]bool{"X": true, "Y": true, "M": true, "L": true, "F": true, "V": true, "B": true}

// spanWords is how many words each data-type code consumes. Keep in sync
// with decodeValue. Types not listed here are not block-eligible.
//
//	1 uint16, 5 int16        → 1 word
//	2 float32, 7 int32       → 2 words
//	4 ASCII                  → 4 words (decodeValue currently only uses the
//	                           first word; reading 4 matches the old request
//	                           so a future ASCII fix still works)
//	3 bit                    → 1 point (bit devices) / 1 word (word devices)
var spanWords = map[int]int64{1: 1, 2: 2, 3: 1, 4: 4, 5: 1, 7: 2}

// parseDeviceNumber mirrors ReadData's addressing rules exactly.
func parseDeviceNumber(deviceType, deviceNumber string) (int64, error) {
	if deviceType == "Y" || deviceType == "W" {
		return strconv.ParseInt(deviceNumber, 16, 64)
	}
	return strconv.ParseInt(deviceNumber, 10, 64)
}

// PlanBlocks groups devices into batch reads. fx=true returns everything
// as singles.
func PlanBlocks(devices []PLC_Utils.Device, fx bool, opt PlanOptions) *BlockPlan {
	opt = opt.withDefaults()
	plan := &BlockPlan{}
	if fx {
		plan.Singles = append(plan.Singles, devices...)
		return plan
	}

	byType := map[string][]BlockMember{}
	var order []string // keep device-type order stable for logs

	for _, d := range devices {
		m, ok := memberFor(d)
		if !ok {
			plan.Singles = append(plan.Singles, d)
			continue
		}
		if _, seen := byType[d.DeviceType]; !seen {
			order = append(order, d.DeviceType)
		}
		byType[d.DeviceType] = append(byType[d.DeviceType], m)
	}

	for _, dt := range order {
		members := byType[dt]
		sort.SliceStable(members, func(i, j int) bool { return members[i].start < members[j].start })

		isBit := bitDevices[dt]
		unit := int64(1) // units per word
		if isBit {
			unit = 16
		}
		maxUnits := opt.MaxBlockWords * unit
		maxGap := opt.MaxGapWords * unit

		var cur *Block
		var curStart, curEnd int64
		flush := func() {
			if cur == nil {
				return
			}
			cur.Start = curStart
			cur.Words = (curEnd - curStart + unit - 1) / unit
			plan.Blocks = append(plan.Blocks, cur)
			cur = nil
		}

		for _, m := range members {
			if cur != nil {
				newEnd := max(curEnd, m.end)
				if m.start-curEnd <= maxGap && newEnd-curStart <= maxUnits {
					cur.Members = append(cur.Members, m)
					curEnd = newEnd
					continue
				}
				flush()
			}
			curStart = m.start
			if isBit {
				curStart -= curStart % 16 // align to a word boundary
			}
			curEnd = m.end
			cur = &Block{DeviceType: dt, BitDevice: isBit, Members: []BlockMember{m}}
		}
		flush()
	}
	return plan
}

func memberFor(d PLC_Utils.Device) (BlockMember, bool) {
	if _, known := deviceCodesFor3E(d.DeviceType); !known {
		return BlockMember{}, false
	}
	typ := int(d.NumberRegisters)
	words, ok := spanWords[typ]
	if !ok {
		return BlockMember{}, false
	}
	addr, err := parseDeviceNumber(d.DeviceType, d.DeviceNumber)
	if err != nil || addr < 0 {
		return BlockMember{}, false
	}

	m := BlockMember{Device: d, addr: addr, typ: typ}
	switch {
	case !bitDevices[d.DeviceType]:
		m.start, m.end = addr, addr+words
	case typ == 3:
		m.start, m.end = addr, addr+1 // one bit point
	case addr%16 == 0:
		m.start, m.end = addr, addr+words*16
	default:
		return BlockMember{}, false // unaligned word value on a bit device
	}
	return m, true
}

// deviceCodesFor3E reports whether the 3E request builder can address
// this device type. Kept as a func so the planner doesn't reach into mcp
// internals.
func deviceCodesFor3E(deviceType string) (string, bool) {
	return mcp.DeviceCode(deviceType)
}

// ReadBlock performs one batch read and decodes every member.
// A returned error means the whole block failed (connection, PLC end code,
// short payload); per-member decode errors are reported in BlockValue.Err.
func (m *MSPClient) ReadBlock(ctx context.Context, b *Block) ([]BlockValue, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("MSP client not initialized")
	}

	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1) // buffered: no goroutine leak if ctx wins
	go func() {
		data, err := m.client.Read(b.DeviceType, b.Start, b.Words, false)
		ch <- result{data, err}
	}()

	var raw []byte
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		raw = r.data
	}

	resp, err := mcp.NewParser().Do(raw)
	if err != nil {
		return nil, err
	}
	if resp.EndCode != "0000" {
		return nil, fmt.Errorf("block %s%d x%d: PLC error end code %s", b.DeviceType, b.Start, b.Words, resp.EndCode)
	}
	payload := resp.Payload
	if int64(len(payload)) < b.Words*2 {
		return nil, fmt.Errorf("block %s%d x%d: short payload %d bytes, want %d",
			b.DeviceType, b.Start, b.Words, len(payload), b.Words*2)
	}

	out := make([]BlockValue, 0, len(b.Members))
	for _, mem := range b.Members {
		v, err := extractMember(payload, b, mem)
		out = append(out, BlockValue{Device: mem.Device, Value: v, Err: err})
	}
	return out, nil
}

func extractMember(payload []byte, b *Block, mem BlockMember) (any, error) {
	if b.BitDevice {
		rel := mem.addr - b.Start
		if mem.typ == 3 {
			// Word access to bit devices: point n is bit (n%8) of byte n/8.
			return uint8((payload[rel/8] >> (rel % 8)) & 0x01), nil
		}
		off := rel / 16 * 2
		return decodeValue(payload[off:off+spanWords[mem.typ]*2], mem.typ)
	}
	off := (mem.addr - b.Start) * 2
	return decodeValue(payload[off:off+spanWords[mem.typ]*2], mem.typ)
}
