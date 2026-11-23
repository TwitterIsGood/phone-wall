package main

import (
	"bytes"
	"encoding/binary"
	"testing"
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
	if p[0] != 0 || p[1] != 0 {
		t.Fatalf("header = %v", p[:2])
	}
	if code := binary.BigEndian.Uint32(p[2:]); code != 187 {
		t.Fatalf("keycode = %d", code)
	}
}
