package main

import "testing"

func TestScaleToNatural(t *testing.T) {
	// Portrait frame on a portrait panel: identity.
	x, y, sw, sh := scaleToNatural(100, 200, 1080, 2400, 1080, 2400)
	if x != 100 || y != 200 || sw != 1080 || sh != 2400 {
		t.Fatalf("identity = %d,%d %dx%d", x, y, sw, sh)
	}
	// Landscape frame on a portrait panel: axes are swapped.
	x, y, sw, sh = scaleToNatural(100, 200, 2400, 1080, 1080, 2400)
	if x != 100 || y != 200 || sw != 2400 || sh != 1080 {
		t.Fatalf("landscape = %d,%d %dx%d", x, y, sw, sh)
	}
	// Half-size frame is scaled up to the panel.
	x, y, _, _ = scaleToNatural(50, 100, 540, 1200, 1080, 2400)
	if x != 100 || y != 200 {
		t.Fatalf("scaled = %d,%d", x, y)
	}
	// Points outside the frame are clamped.
	x, y, _, _ = scaleToNatural(-5, 99999, 1080, 2400, 1080, 2400)
	if x != 0 || y != 2399 {
		t.Fatalf("clamped = %d,%d", x, y)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"abc":      "'abc'",
		"a b":      "'a b'",
		"it's":     `'it'\''s'`,
		"; reboot": "'; reboot'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
