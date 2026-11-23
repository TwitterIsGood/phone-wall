package main

import (
	"context"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	adbPath string

	regMu     sync.RWMutex
	devices   []Device
	statusMap = map[string]string{}
	activeDev = map[string]string{}
	realSize  = map[string][2]int{}
)

func adbOut(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, adbPath, args...).Output()
	return string(out), err
}

func adbBytes(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, adbPath, args...).Output()
}

func adbFor(dev string, args ...string) []string {
	return append([]string{"-s", dev}, args...)
}

// runAdb is best-effort: input events have no useful return value.
func runAdb(dev string, args ...string) {
	_ = exec.Command(adbPath, adbFor(dev, append([]string{"shell"}, args...)...)...).Run()
}

func findDevice(key string) (Device, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	for _, d := range devices {
		if d.Key == key {
			return d, true
		}
	}
	return Device{}, false
}

func listDevices() []Device {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Device, len(devices))
	copy(out, devices)
	return out
}

func activeSerial(key string) string {
	regMu.RLock()
	defer regMu.RUnlock()
	if s := activeDev[key]; s != "" {
		return s
	}
	for _, d := range devices {
		if d.Key == key && len(d.Serials) > 0 {
			return d.Serials[0]
		}
	}
	return ""
}

// statusWorker keeps the dot colour and the serial actually in use up to date.
func statusWorker() {
	for {
		out, err := adbOut(5*time.Second, "devices")
		present := map[string]bool{}
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				f := strings.Fields(line)
				if len(f) >= 2 && f[1] == "device" {
					present[f[0]] = true
				}
			}
		}
		regMu.Lock()
		for _, d := range devices {
			found := ""
			for _, serial := range d.Serials {
				if present[serial] {
					found = serial
					break
				}
			}
			if found != "" {
				activeDev[d.Key] = found
				statusMap[d.Key] = "device"
			} else {
				delete(activeDev, d.Key)
				delete(realSize, d.Key)
				statusMap[d.Key] = "offline"
			}
		}
		regMu.Unlock()
		time.Sleep(2 * time.Second)
	}
}

func getRealSize(key, dev string) (int, int) {
	regMu.RLock()
	sz, ok := realSize[key]
	regMu.RUnlock()
	if ok && sz[0] > 0 && sz[1] > 0 {
		return sz[0], sz[1]
	}
	out, err := adbOut(6*time.Second, adbFor(dev, "shell", "wm", "size")...)
	if err == nil {
		if m := regexp.MustCompile(`(\d+)x(\d+)`).FindStringSubmatch(out); m != nil {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			if w > 0 && h > 0 {
				regMu.Lock()
				realSize[key] = [2]int{w, h}
				regMu.Unlock()
				return w, h
			}
		}
	}
	return 1080, 2400
}

// scaleToNatural maps a point in a frame's coordinate space to the device's
// natural (portrait) display. The frame may be rotated relative to the panel.
func scaleToNatural(x, y, fw, fh float64, rw, rh int) (int, int, int, int) {
	sw, sh := rw, rh
	if fw > fh {
		sw, sh = rh, rw
	}
	scale := 1.0
	if fw > 0 {
		scale = float64(sw) / fw
	}
	xd := int(x * scale)
	yd := int(y * scale)
	if xd < 0 {
		xd = 0
	}
	if yd < 0 {
		yd = 0
	}
	if xd > sw-1 {
		xd = sw - 1
	}
	if yd > sh-1 {
		yd = sh - 1
	}
	return xd, yd, sw, sh
}

func scaleToDevice(key, dev string, x, y, fw, fh float64) (int, int, int, int) {
	rw, rh := getRealSize(key, dev)
	return scaleToNatural(x, y, fw, fh, rw, rh)
}

func cleanForwards() {
	out, err := adbOut(5*time.Second, "forward", "--list")
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.Contains(f[len(f)-1], "localabstract:scrcpy_") {
			adbOut(8*time.Second, "-s", f[0], "forward", "--remove", f[1])
		}
	}
}

func removeForward(dev string, port int) {
	if port > 0 && dev != "" {
		adbOut(8*time.Second, "-s", dev, "forward", "--remove", "tcp:"+strconv.Itoa(port))
	}
}

// ---- HTTP handlers -------------------------------------------------------

func statusHandler(w http.ResponseWriter, _ *http.Request) {
	regMu.RLock()
	st := make(map[string]string, len(statusMap))
	for k, v := range statusMap {
		st[k] = v
	}
	regMu.RUnlock()
	jsonReply(w, map[string]any{"status": st})
}

func devicesHandler(w http.ResponseWriter, _ *http.Request) {
	ds := listDevices()
	out := make([]map[string]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, map[string]string{"key": d.Key, "name": displayName(d.Key, d.Name)})
	}
	jsonReply(w, map[string]any{"devices": out})
}

func renameHandler(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	d, ok := findDevice(key)
	if !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if runes := []rune(name); len(runes) > 48 {
		name = string(runes[:48])
	}
	setDisplayName(key, name)
	jsonReply(w, map[string]any{"ok": true, "key": key, "name": displayName(key, d.Name)})
}

func requireDevice(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	key := r.PathValue("key")
	if _, ok := findDevice(key); !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return "", "", false
	}
	dev := activeSerial(key)
	if dev == "" {
		http.Error(w, "device offline", http.StatusServiceUnavailable)
		return "", "", false
	}
	return key, dev, true
}

func tapHandler(w http.ResponseWriter, r *http.Request) {
	key, dev, ok := requireDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	x, _ := strconv.ParseFloat(q.Get("x"), 64)
	y, _ := strconv.ParseFloat(q.Get("y"), 64)
	fw, _ := strconv.ParseFloat(q.Get("fw"), 64)
	fh, _ := strconv.ParseFloat(q.Get("fh"), 64)
	xd, yd, _, _ := scaleToDevice(key, dev, x, y, fw, fh)
	runAdb(dev, "input", "keyevent", "KEYCODE_WAKEUP")
	runAdb(dev, "input", "tap", strconv.Itoa(xd), strconv.Itoa(yd))
	jsonReply(w, map[string]any{"ok": true})
}

func swipeHandler(w http.ResponseWriter, r *http.Request) {
	key, dev, ok := requireDevice(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	fw, _ := strconv.ParseFloat(q.Get("fw"), 64)
	fh, _ := strconv.ParseFloat(q.Get("fh"), 64)
	x1, _ := strconv.ParseFloat(q.Get("x1"), 64)
	y1, _ := strconv.ParseFloat(q.Get("y1"), 64)
	x2, _ := strconv.ParseFloat(q.Get("x2"), 64)
	y2, _ := strconv.ParseFloat(q.Get("y2"), 64)
	dur, _ := strconv.Atoi(q.Get("d"))
	if dur <= 0 {
		dur = 250
	}
	ax, ay, _, _ := scaleToDevice(key, dev, x1, y1, fw, fh)
	bx, by, _, _ := scaleToDevice(key, dev, x2, y2, fw, fh)
	runAdb(dev, "input", "keyevent", "KEYCODE_WAKEUP")
	runAdb(dev, "input", "swipe", strconv.Itoa(ax), strconv.Itoa(ay), strconv.Itoa(bx), strconv.Itoa(by), strconv.Itoa(dur))
	jsonReply(w, map[string]any{"ok": true})
}

var adbKeycodes = map[string]string{
	"back":    "KEYCODE_BACK",
	"home":    "KEYCODE_HOME",
	"recents": "KEYCODE_APP_SWITCH",
	"enter":   "KEYCODE_ENTER",
	"wake":    "KEYCODE_WAKEUP",
}

func keyHandler(w http.ResponseWriter, r *http.Request) {
	_, dev, ok := requireDevice(w, r)
	if !ok {
		return
	}
	k := r.URL.Query().Get("k")
	code := adbKeycodes[k]
	if code == "" {
		http.Error(w, "unknown key", http.StatusBadRequest)
		return
	}
	runAdb(dev, "input", "keyevent", code)
	if k == "wake" {
		runAdb(dev, "wm", "dismiss-keyguard")
	}
	jsonReply(w, map[string]any{"ok": true})
}

// shellQuote keeps user text from being interpreted by the adb shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func textHandler(w http.ResponseWriter, r *http.Request) {
	_, dev, ok := requireDevice(w, r)
	if !ok {
		return
	}
	t := strings.ReplaceAll(r.URL.Query().Get("t"), " ", "%s")
	runAdb(dev, "input", "text", shellQuote(t))
	jsonReply(w, map[string]any{"ok": true})
}

func frameHandler(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if _, ok := findDevice(key); !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	dev := activeSerial(key)
	if dev == "" {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	b, err := adbBytes(20*time.Second, adbFor(dev, "exec-out", "screencap", "-p")...)
	if err != nil || len(b) == 0 {
		http.Error(w, "waiting for frame", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}
