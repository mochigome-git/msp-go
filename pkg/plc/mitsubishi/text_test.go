package mitsubishi

import "testing"

// Words are given as they arrive from the PLC: lo byte first, then hi.
func TestDecodeTextWord(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi byte
		enc    TextEncoding
		want   string
	}{
		// ASCII keeps the original high-byte-first output.
		{"ascii pair", 'A', 'B', TextAuto, "BA"},
		{"ascii with NUL", 'A', 0x00, TextAuto, "\x00A"},
		// Shift-JIS kanji: 記 = 8B 4C (the W50 value that showed as "L�").
		{"sjis kanji 記", 0x8B, 0x4C, TextAuto, "記"},
		{"sjis kanji 漢", 0x8A, 0xBF, TextAuto, "漢"},
		{"sjis hiragana あ", 0x82, 0xA0, TextAuto, "あ"},
		{"sjis full-width space", 0x81, 0x40, TextAuto, "　"},
		{"half-width katakana pair", 0xB1, 0xB2, TextAuto, "ｲｱ"},
		{"ascii + half-width kana", 'A', 0xB1, TextAuto, "ｱA"},
		// Kanji split across registers: no invalid UTF-8 leaks out.
		{"split kanji", 'A', 0x8B, TextAuto, "�A"},
		// Unicode (UTF-16LE): 記 = U+8A18, 一 = U+4E00, 'A' = U+0041.
		{"utf16 kanji 記", 0x18, 0x8A, TextUnicode, "記"},
		{"utf16 kanji 一", 0x00, 0x4E, TextUnicode, "一"},
		{"utf16 ascii", 'A', 0x00, TextUnicode, "A"},
		{"utf16 lone surrogate", 0x3D, 0xD8, TextUnicode, "�"},
	}
	for _, c := range cases {
		if got := decodeTextWord(c.lo, c.hi, c.enc); got != c.want {
			t.Errorf("%s: decodeTextWord(%#02x, %#02x) = %q, want %q", c.name, c.lo, c.hi, got, c.want)
		}
	}
}

func TestDecodeValueText(t *testing.T) {
	// Type 4 reads 4 words but only the first is decoded.
	data := []byte{0x8B, 0x4C, 0x98, 0x5E, 0, 0, 0, 0}
	v, err := decodeValue(data, 4, TextAuto)
	if err != nil {
		t.Fatal(err)
	}
	if v != "記" {
		t.Fatalf("got %q, want %q", v, "記")
	}
}

func TestParseTextEncoding(t *testing.T) {
	for in, want := range map[string]TextEncoding{
		"": TextAuto, "auto": TextAuto, "SJIS": TextAuto, "unicode": TextUnicode, "UTF-16": TextUnicode,
	} {
		got, err := ParseTextEncoding(in)
		if err != nil || got != want {
			t.Errorf("ParseTextEncoding(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseTextEncoding("latin1"); err == nil {
		t.Error("expected error for unknown encoding")
	}
}
