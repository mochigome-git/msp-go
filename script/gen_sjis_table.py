#!/usr/bin/env python3
"""Generate pkg/plc/mitsubishi/sjis_cp932.bin — the Shift-JIS (CP932)
double-byte → Unicode lookup table used to decode kanji read from PLC
registers (data type 4).

Layout: 60 lead bytes (0x81-0x9F, 0xE0-0xFC) x 188 trail slots
(0x40-0x7E, 0x80-0xFC), one little-endian uint16 code point each.
0 means "not a mapped character". User-defined area (lead 0xF0-0xF9,
mapped by Python to the Private Use Area) is left unmapped.

Run from the repo root:  python3 script/gen_sjis_table.py
"""
import struct

LEADS = list(range(0x81, 0xA0)) + list(range(0xE0, 0xFD))
TRAILS = list(range(0x40, 0x7F)) + list(range(0x80, 0xFD))
assert len(LEADS) == 60 and len(TRAILS) == 188

out = bytearray()
mapped = 0
for lead in LEADS:
    for trail in TRAILS:
        cp = 0
        if not 0xF0 <= lead <= 0xF9:
            try:
                s = bytes([lead, trail]).decode("cp932")
                if len(s) == 1 and ord(s) <= 0xFFFF:
                    cp = ord(s)
            except UnicodeDecodeError:
                pass
        if cp:
            mapped += 1
        out += struct.pack("<H", cp)

with open("pkg/plc/mitsubishi/sjis_cp932.bin", "wb") as f:
    f.write(out)
print(f"wrote {len(out)} bytes, {mapped} mapped characters")
