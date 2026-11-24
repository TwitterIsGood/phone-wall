package main

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNALHeader(t *testing.T) {
	if n := nalHeader([]byte{0, 0, 0, 1, 0x67}); n != 4 {
		t.Fatalf("4-byte start code = %d", n)
	}
	if n := nalHeader([]byte{0, 0, 1, 0x67}); n != 3 {
		t.Fatalf("3-byte start code = %d", n)
	}
	if n := nalHeader([]byte{1, 2, 3}); n != -1 {
		t.Fatalf("no start code = %d", n)
	}
}

func TestSplitNALs(t *testing.T) {
	// Two complete NAL units and a partial third.
	buf := []byte{
		0, 0, 0, 1, 0x67, 0xAA,
		0, 0, 1, 0x68, 0xBB,
		0, 0, 0, 1, 0x65, 0xCC,
	}
	nals, rest := splitNALs(buf)
	if len(nals) != 2 {
		t.Fatalf("complete NALs = %d, want 2", len(nals))
	}
	if !bytes.Equal(nals[0], []byte{0, 0, 0, 1, 0x67, 0xAA}) {
		t.Fatalf("nal 0 = %x", nals[0])
	}
	if !bytes.Equal(nals[1], []byte{0, 0, 1, 0x68, 0xBB}) {
		t.Fatalf("nal 1 = %x", nals[1])
	}
	if !bytes.Equal(rest, []byte{0, 0, 0, 1, 0x65, 0xCC}) {
		t.Fatalf("rest = %x", rest)
	}
}

func TestSplitNALsTrimsTrailingZeros(t *testing.T) {
	buf := []byte{
		0, 0, 1, 0x67, 0xAA, 0, 0,
		0, 0, 1, 0x68, 0xBB,
	}
	nals, _ := splitNALs(buf)
	if len(nals) != 1 {
		t.Fatalf("got %d NALs", len(nals))
	}
	if !bytes.Equal(nals[0], []byte{0, 0, 1, 0x67, 0xAA}) {
		t.Fatalf("trailing zeros not trimmed: %x", nals[0])
	}
}

func TestTouchPacket(t *testing.T) {
	down := touchPacket(0, 100, 200, 1080, 2400)
	if len(down) != 32 {
		t.Fatalf("size = %d", len(down))
	}
	if down[0] != 2 || down[1] != 0 {
		t.Fatalf("header = %v", down[:2])
	}
	if pid := binary.BigEndian.Uint64(down[2:]); pid != 1 {
		t.Fatalf("pointer id = %d", pid)
	}
	if x, y := binary.BigEndian.Uint32(down[10:]), binary.BigEndian.Uint32(down[14:]); x != 100 || y != 200 {
		t.Fatalf("point = %d,%d", x, y)
	}
	if w, h := binary.BigEndian.Uint16(down[18:]), binary.BigEndian.Uint16(down[20:]); w != 1080 || h != 2400 {
		t.Fatalf("size = %d,%d", w, h)
	}
	if p := binary.BigEndian.Uint16(down[22:]); p != 0xFFFF {
		t.Fatalf("down pressure = %#x", p)
	}
	up := touchPacket(1, 100, 200, 1080, 2400)
	if p := binary.BigEndian.Uint16(up[22:]); p != 0 {
		t.Fatalf("up pressure = %#x", p)
	}
}

func TestKeyPacket(t *testing.T) {
	p := keyPacket(0, 187)
	if len(p) != 14 {
		t.Fatalf("size = %d", len(p))
	}
	if p[0] != ctrlInjectKeycode || p[1] != 0 {
		t.Fatalf("header = %v", p[:2])
	}
	if code := binary.BigEndian.Uint32(p[2:]); code != 187 {
		t.Fatalf("keycode = %d", code)
	}
	if code := keycodes["del"]; code != 67 {
		t.Fatalf("del keycode = %d, want 67", code)
	}
	if code := keycodes["enter"]; code != 66 {
		t.Fatalf("enter keycode = %d, want 66", code)
	}
}

func TestInjectTextPacket(t *testing.T) {
	p := injectTextPacket("hello")
	if len(p) != 5+5 {
		t.Fatalf("size = %d", len(p))
	}
	if p[0] != ctrlInjectText {
		t.Fatalf("type = %d", p[0])
	}
	if n := binary.BigEndian.Uint32(p[1:]); n != 5 {
		t.Fatalf("length = %d", n)
	}
	if got := string(p[5:]); got != "hello" {
		t.Fatalf("text = %q", got)
	}
}

func TestSetClipboardPacket(t *testing.T) {
	const text = "你好123"
	p := setClipboardPacket(text)
	if len(p) != 14+len(text) {
		t.Fatalf("size = %d", len(p))
	}
	if p[0] != ctrlSetClipboard {
		t.Fatalf("type = %d", p[0])
	}
	for i := 1; i <= 8; i++ {
		if p[i] != 0 {
			t.Fatalf("sequence not zero: %x", p[1:9])
		}
	}
	if p[9] != 1 {
		t.Fatalf("paste flag = %d", p[9])
	}
	if n := binary.BigEndian.Uint32(p[10:]); int(n) != len(text) {
		t.Fatalf("length = %d, want %d", n, len(text))
	}
	if got := string(p[14:]); got != text {
		t.Fatalf("text = %q", got)
	}
}

func TestTextPackets(t *testing.T) {
	if got := textPackets(""); got != nil {
		t.Fatalf("empty text = %v", got)
	}

	// ASCII uses inject-text and is chunked at 300 bytes per packet.
	long := strings.Repeat("x", 700)
	packets := textPackets(long)
	if len(packets) != 3 {
		t.Fatalf("long ascii packets = %d, want 3", len(packets))
	}
	var joined []byte
	for i, p := range packets {
		if p[0] != ctrlInjectText {
			t.Fatalf("packet %d type = %d", i, p[0])
		}
		if len(p)-5 > maxInjectTextBytes {
			t.Fatalf("packet %d payload = %d bytes", i, len(p)-5)
		}
		joined = append(joined, p[5:]...)
	}
	if string(joined) != long {
		t.Fatalf("chunks do not reassemble: %d bytes", len(joined))
	}

	// Any non-ASCII rune switches to clipboard + paste, which the device can
	// deliver as Unicode instead of KeyCharacterMap key events.
	for _, text := range []string{"你好", "abc中文", "line\nbreak", "café"} {
		packets := textPackets(text)
		if len(packets) != 1 || packets[0][0] != ctrlSetClipboard {
			t.Fatalf("%q did not use the clipboard path", text)
		}
		if got := string(packets[0][14:]); got != text {
			t.Fatalf("%q payload = %q", text, got)
		}
	}
}

func TestChunkUTF8StaysOnRuneBoundaries(t *testing.T) {
	text := strings.Repeat("中", 5) // 3 bytes per rune
	chunks := chunkUTF8(text, 7)
	var joined strings.Builder
	for _, c := range chunks {
		if len(c) > 7 {
			t.Fatalf("chunk %q is %d bytes", c, len(c))
		}
		if !utf8.ValidString(c) {
			t.Fatalf("chunk split a rune: %x", c)
		}
		joined.WriteString(c)
	}
	if joined.String() != text {
		t.Fatalf("chunks do not reassemble")
	}
}

func TestClipTextKeepsValidUTF8(t *testing.T) {
	text := strings.Repeat("中", maxTextBytes/3+10)
	got := clipText(text)
	if len(got) > maxTextBytes {
		t.Fatalf("clipped to %d bytes", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("clip split a rune")
	}
	if !strings.HasPrefix(text, got) {
		t.Fatalf("clip is not a prefix")
	}
}
