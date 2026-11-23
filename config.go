package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Device is one tile. Key is stable and appears in URLs; Serials are tried in
// order, so a device that is reachable both over USB and wireless can list both.
type Device struct {
	Key     string   `json:"key"`
	Name    string   `json:"name,omitempty"`
	Serials []string `json:"serials"`
}

type fileConfig struct {
	Devices []Device `json:"devices"`
}

var keyRe = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

func loadDeviceConfig(path string) ([]Device, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c fileConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateDevices(c.Devices); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c.Devices, nil
}

func validateDevices(ds []Device) error {
	seen := map[string]bool{}
	for i := range ds {
		d := &ds[i]
		d.Key = strings.TrimSpace(d.Key)
		d.Name = strings.TrimSpace(d.Name)
		if d.Key == "" {
			return fmt.Errorf("device %d: empty key", i+1)
		}
		if !keyRe.MatchString(d.Key) {
			return fmt.Errorf("device %q: key may only contain letters, digits, '.', '_', ':' and '-'", d.Key)
		}
		if seen[d.Key] {
			return fmt.Errorf("duplicate device key %q", d.Key)
		}
		seen[d.Key] = true
		if len(d.Serials) == 0 {
			return fmt.Errorf("device %q: no serials", d.Key)
		}
		for j := range d.Serials {
			d.Serials[j] = strings.TrimSpace(d.Serials[j])
			if d.Serials[j] == "" {
				return fmt.Errorf("device %q: empty serial", d.Key)
			}
		}
		if d.Name == "" {
			d.Name = d.Key
		}
	}
	return nil
}

// loadDevices loads devices.json, or falls back to adb discovery when the file
// does not exist. Auto-discovery keys devices by serial and names them after
// the adb model, which is enough for a first run.
func loadDevices(path string) error {
	ds, err := loadDeviceConfig(path)
	if os.IsNotExist(err) {
		ds, err = discoverDevices()
		if err != nil {
			return fmt.Errorf("no %s and adb discovery failed: %w", path, err)
		}
		log.Printf("no %s; discovered %d device(s) with adb", path, len(ds))
	} else if err != nil {
		return err
	}
	regMu.Lock()
	devices = ds
	regMu.Unlock()
	return nil
}

func discoverDevices() ([]Device, error) {
	out, err := adbOut(8*time.Second, "devices", "-l")
	if err != nil {
		return nil, err
	}
	entries := parseADBDevices(out)

	// The same phone can show up twice: once over USB and once over wireless
	// debugging. ro.serialno is the hardware serial and lets us merge them.
	hw := make([]string, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i := range entries {
		serial := entries[i].Serials[0]
		if strings.Contains(serial, "_adb-tls-connect._tcp") {
			continue
		}
		wg.Add(1)
		go func(i int, serial string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s, err := adbOut(4*time.Second, "-s", serial, "shell", "getprop", "ro.serialno"); err == nil {
				hw[i] = strings.TrimSpace(s)
			}
		}(i, serial)
	}
	wg.Wait()

	bySerial := make(map[string]string, len(entries))
	for i, e := range entries {
		bySerial[e.Serials[0]] = hw[i]
	}
	return dedupeDevices(entries, func(serial string) string { return bySerial[serial] }), nil
}

// parseADBDevices understands the output of `adb devices -l`.
func parseADBDevices(out string) []Device {
	var ds []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") || strings.HasPrefix(line, "*") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 || f[1] != "device" {
			continue
		}
		serial := f[0]
		model := ""
		for _, field := range f[2:] {
			if strings.HasPrefix(field, "model:") {
				model = strings.ReplaceAll(strings.TrimPrefix(field, "model:"), "_", " ")
				break
			}
		}
		if model == "" {
			model = serial
		}
		ds = append(ds, Device{Key: serial, Name: model, Serials: []string{serial}})
	}
	return ds
}

// dedupeDevices merges entries that report the same hardware serial, prefers
// the USB transport, and disambiguates identical model names.
func dedupeDevices(entries []Device, hardwareSerial func(string) string) []Device {
	type group struct {
		hw      string
		model   string
		serials []string
	}
	groups := map[string]*group{}
	var order []string
	for _, e := range entries {
		serial := e.Serials[0]
		if strings.Contains(serial, "_adb-tls-connect._tcp") {
			continue
		}
		hw := hardwareSerial(serial)
		if hw == "" {
			hw = serial
		}
		g := groups[hw]
		if g == nil {
			g = &group{hw: hw, model: e.Name}
			groups[hw] = g
			order = append(order, hw)
		}
		g.serials = append(g.serials, serial)
	}

	out := make([]Device, 0, len(order))
	for _, hw := range order {
		g := groups[hw]
		// Prefer a USB serial so startup is not blocked by a flaky Wi-Fi link.
		sort.SliceStable(g.serials, func(i, j int) bool {
			return !strings.Contains(g.serials[i], ":") && strings.Contains(g.serials[j], ":")
		})
		key := g.hw
		if key == "" || !keyRe.MatchString(key) {
			key = g.serials[0]
		}
		out = append(out, Device{Key: key, Name: g.model, Serials: g.serials})
	}

	counts := map[string]int{}
	for _, d := range out {
		counts[d.Name]++
	}
	for i := range out {
		if counts[out[i].Name] > 1 {
			suffix := out[i].Key
			if len(suffix) > 4 {
				suffix = suffix[len(suffix)-4:]
			}
			out[i].Name = out[i].Name + " · " + suffix
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var (
	namesMu      sync.Mutex
	namesFile    string
	displayNames = map[string]string{}
)

func loadNames(path string) {
	namesFile = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(b, &m) == nil && m != nil {
		namesMu.Lock()
		displayNames = m
		namesMu.Unlock()
	}
}

func saveNames() error {
	namesMu.Lock()
	b, _ := json.MarshalIndent(displayNames, "", " ")
	namesMu.Unlock()
	return os.WriteFile(namesFile, b, 0o644)
}

func displayName(key, fallback string) string {
	namesMu.Lock()
	defer namesMu.Unlock()
	if n := strings.TrimSpace(displayNames[key]); n != "" {
		return n
	}
	return fallback
}

func setDisplayName(key, name string) {
	namesMu.Lock()
	if name == "" {
		delete(displayNames, key)
	} else {
		displayNames[key] = name
	}
	namesMu.Unlock()
	_ = saveNames()
}
