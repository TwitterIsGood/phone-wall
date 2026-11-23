package main

import "testing"

func TestValidateDevices(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		ds := []Device{{Key: "pixel-1", Serials: []string{"A", "192.0.2.10:5555"}}}
		if err := validateDevices(ds); err != nil {
			t.Fatal(err)
		}
		if ds[0].Name != "pixel-1" {
			t.Fatalf("name default = %q", ds[0].Name)
		}
	})
	t.Run("empty key", func(t *testing.T) {
		if err := validateDevices([]Device{{Serials: []string{"A"}}}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("slash in key", func(t *testing.T) {
		if err := validateDevices([]Device{{Key: "a/b", Serials: []string{"A"}}}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("duplicate key", func(t *testing.T) {
		ds := []Device{{Key: "a", Serials: []string{"A"}}, {Key: "a", Serials: []string{"B"}}}
		if err := validateDevices(ds); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("no serials", func(t *testing.T) {
		if err := validateDevices([]Device{{Key: "a"}}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestParseADBDevices(t *testing.T) {
	out := `List of devices attached
R58M123456A            device product:shiba model:Pixel_8 device:shiba transport_id:1
192.0.2.25:5555        device product:shiba model:Pixel_8 device:shiba transport_id:2
0123456789ABCDEF       unauthorized usb:1-1
emulator-5554          offline
`
	got := parseADBDevices(out)
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(got), got)
	}
	if got[0].Key != "R58M123456A" || got[0].Name != "Pixel 8" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Key != "192.0.2.25:5555" || got[1].Name != "Pixel 8" {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestDedupeDevices(t *testing.T) {
	entries := []Device{
		{Key: "R58M123456A", Name: "Pixel 8", Serials: []string{"R58M123456A"}},
		{Key: "192.0.2.25:5555", Name: "Pixel 8", Serials: []string{"192.0.2.25:5555"}},
		{Key: "192.0.2.31:5555", Name: "Pixel 8", Serials: []string{"192.0.2.31:5555"}},
		{Key: "adb-R58M123456A-abc._adb-tls-connect._tcp", Name: "Pixel 8", Serials: []string{"adb-R58M123456A-abc._adb-tls-connect._tcp"}},
	}
	hw := map[string]string{
		"R58M123456A":     "HW1",
		"192.0.2.25:5555": "HW1",
		"192.0.2.31:5555": "HW2",
	}
	got := dedupeDevices(entries, func(serial string) string { return hw[serial] })
	if len(got) != 2 {
		t.Fatalf("got %d devices, want 2: %+v", len(got), got)
	}
	if got[0].Key != "HW1" || len(got[0].Serials) != 2 {
		t.Fatalf("merged device = %+v", got[0])
	}
	if got[0].Serials[0] != "R58M123456A" {
		t.Fatalf("USB serial should come first: %+v", got[0].Serials)
	}
	if got[0].Name != "Pixel 8 · HW1" || got[1].Name != "Pixel 8 · HW2" {
		t.Fatalf("names = %q, %q", got[0].Name, got[1].Name)
	}
}
