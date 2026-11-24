package main

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

var (
	scrcpyJar     string
	scrcpyVersion string
)

// scrcpy control message types used by this client. The numbering comes from
// ControlMessage.java / doc/control.md in scrcpy (5.0.1 here).
const (
	ctrlInjectKeycode = 0x00
	ctrlInjectText    = 0x01
	ctrlInjectTouch   = 0x02
	ctrlSetClipboard  = 0x09
)

const (
	// scrcpy's server refuses nothing above this, but its reader documents
	// INJECT_TEXT_MAX_LENGTH = 300 bytes per message, so chunk ASCII input.
	maxInjectTextBytes = 300
	// The clipboard control message tops out near 256 KiB; a smaller cap keeps
	// a rogue same-origin client from pinning memory or the device clipboard.
	maxTextBytes = 16 << 10
)

// keycodes are Android key event codes understood by scrcpy's inject-keycode
// control message. KeyCharacterMap-backed INJECT_TEXT handles printable ASCII;
// these cover editing and navigation keys.
var keycodes = map[string]uint32{
	"back": 4, "home": 3, "recents": 187, "enter": 66,
	"del": 67, "forward_del": 112, "tab": 61, "escape": 111,
	"dpad_up": 19, "dpad_down": 20, "dpad_left": 21, "dpad_right": 22,
	"move_home": 122, "move_end": 123,
}

type session struct {
	dev     string
	video   net.Conn
	control net.Conn
	port    int
	cmd     *exec.Cmd
}

// resolveJar looks for scrcpy-server next to a scrcpy install, in the common
// Winget package layout, or in the working directory.
func resolveJar(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if exe, err := exec.LookPath("scrcpy"); err == nil {
		p := filepath.Join(filepath.Dir(exe), "scrcpy-server")
		if fileExists(p) {
			return p
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		matches, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages",
			"Genymobile.scrcpy*", "scrcpy-win64-*", "scrcpy-server"))
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1]
		}
	}
	if fileExists("scrcpy-server") {
		return "scrcpy-server"
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func ensureJar(dev string) error {
	if scrcpyJar == "" {
		scrcpyJar = resolveJar("")
	}
	if scrcpyJar == "" {
		return fmt.Errorf("scrcpy-server not found: install scrcpy, or pass --scrcpy-jar / SCRCPY_JAR")
	}
	if !fileExists(scrcpyJar) {
		return fmt.Errorf("scrcpy-server not found at %s", scrcpyJar)
	}
	if _, err := adbOut(10*time.Second, adbFor(dev, "shell", "test", "-f", "/data/local/tmp/scrcpy-server.jar")...); err == nil {
		return nil
	}
	if _, err := adbOut(30*time.Second, adbFor(dev, "push", scrcpyJar, "/data/local/tmp/scrcpy-server.jar")...); err != nil {
		return fmt.Errorf("push scrcpy-server: %w", err)
	}
	return nil
}

func startSession(key, dev string, tile bool) (*session, error) {
	runAdb(dev, "input", "keyevent", "KEYCODE_WAKEUP")
	time.Sleep(200 * time.Millisecond)
	if err := ensureJar(dev); err != nil {
		return nil, err
	}
	getRealSize(key, dev)

	scid := fmt.Sprintf("%08x", rand.Int31())
	abstract := "localabstract:scrcpy_" + scid
	out, err := adbOut(15*time.Second, adbFor(dev, "forward", "tcp:0", abstract)...)
	if err != nil {
		return nil, fmt.Errorf("adb forward: %w", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return nil, fmt.Errorf("bad forward port %q", out)
	}

	fps, bitrate, maxSize := "60", "24000000", "0"
	codecOpt := ""
	if tile {
		fps, bitrate, maxSize = "30", "12000000", "1440"
		codecOpt = " video_codec_options=i-frame-interval:int=2"
	}
	cmdStr := fmt.Sprintf("CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server %s scid=%s log_level=error video=true audio=false control=true cleanup=false tunnel_forward=true raw_stream=true stay_awake=true max_fps=%s max_size=%s video_bit_rate=%s%s",
		scrcpyVersion, scid, fps, maxSize, bitrate, codecOpt)
	cmd := exec.Command(adbPath, adbFor(dev, "shell", cmdStr)...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		removeForward(dev, port)
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		removeForward(dev, port)
		return nil, fmt.Errorf("scrcpy server exited: %v", err)
	case <-time.After(1200 * time.Millisecond):
	}

	video, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		cmd.Process.Kill()
		removeForward(dev, port)
		return nil, err
	}
	time.Sleep(100 * time.Millisecond)
	control, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second)
	if err != nil {
		video.Close()
		cmd.Process.Kill()
		removeForward(dev, port)
		return nil, err
	}
	return &session{dev: dev, video: video, control: control, port: port, cmd: cmd}, nil
}

func (s *session) close() {
	if s == nil {
		return
	}
	if s.video != nil {
		s.video.Close()
	}
	if s.control != nil {
		s.control.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	removeForward(s.dev, s.port)
}

// touchPacket is scrcpy's inject-touch control message (32 bytes).
// Layout: type, action, pointerId(u64), x(u32), y(u32), screenW(u16),
// screenH(u16), pressure(u16 fixed point), actionButton(u32), buttons(u32).
func touchPacket(action byte, x, y, sw, sh int) []byte {
	b := make([]byte, 32)
	b[0] = ctrlInjectTouch
	b[1] = action
	binary.BigEndian.PutUint64(b[2:], 1)
	binary.BigEndian.PutUint32(b[10:], uint32(x))
	binary.BigEndian.PutUint32(b[14:], uint32(y))
	binary.BigEndian.PutUint16(b[18:], uint16(sw))
	binary.BigEndian.PutUint16(b[20:], uint16(sh))
	pressure := uint16(0xFFFF)
	if action == 1 { // ACTION_UP
		pressure = 0
	}
	binary.BigEndian.PutUint16(b[22:], pressure)
	return b
}

// keyPacket is scrcpy's inject-keycode control message (14 bytes).
// Layout: type, action, keycode(u32), repeat(u32), metaState(u32).
func keyPacket(action byte, code uint32) []byte {
	b := make([]byte, 14)
	b[0] = ctrlInjectKeycode
	b[1] = action
	binary.BigEndian.PutUint32(b[2:], code)
	return b
}

// injectTextPacket is scrcpy's inject-text control message:
// type(1), length(u32, UTF-8 bytes), text.
//
// On the device it goes through KeyCharacterMap.getEvents(), character by
// character, so it only carries characters a hardware keyboard can produce.
// Non-ASCII text (Chinese, emoji, …) is handled by setClipboardPacket instead.
func injectTextPacket(text string) []byte {
	b := make([]byte, 5+len(text))
	b[0] = ctrlInjectText
	binary.BigEndian.PutUint32(b[1:], uint32(len(text)))
	copy(b[5:], text)
	return b
}

// setClipboardPacket is scrcpy's set-clipboard control message:
// type(1), sequence(u64), paste(1), length(u32), text. sequence 0 means no
// acknowledgement is requested; paste=1 makes the server press KEYCODE_PASTE
// after setting the clipboard, which is how arbitrary Unicode reaches the
// focused field. The device clipboard is overwritten (any app can read it),
// so callers should not send secrets this way without the user knowing.
func setClipboardPacket(text string) []byte {
	b := make([]byte, 14+len(text))
	b[0] = ctrlSetClipboard
	b[9] = 1 // paste
	binary.BigEndian.PutUint32(b[10:], uint32(len(text)))
	copy(b[14:], text)
	return b
}

// isASCIIText reports whether every byte is printable ASCII. Tabs, newlines
// and non-ASCII runes take the clipboard path because KeyCharacterMap cannot
// turn them into key events reliably.
func isASCIIText(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// clipText caps text at maxTextBytes without splitting a UTF-8 rune.
func clipText(s string) string {
	if len(s) <= maxTextBytes {
		return s
	}
	cut := maxTextBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// chunkUTF8 splits s into pieces of at most max bytes, on rune boundaries.
func chunkUTF8(s string, max int) []string {
	var out []string
	for len(s) > 0 {
		n := 0
		for n < len(s) {
			_, size := utf8.DecodeRuneInString(s[n:])
			if n+size > max {
				break
			}
			n += size
		}
		if n == 0 { // a single rune bigger than max (max < 4); never happens
			_, n = utf8.DecodeRuneInString(s)
		}
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

// textPackets encodes user text for the scrcpy control channel: printable
// ASCII goes as chunked INJECT_TEXT messages, anything else as one clipboard
// message plus paste. Tests pin the wire format.
func textPackets(text string) [][]byte {
	text = clipText(text)
	if text == "" {
		return nil
	}
	if !isASCIIText(text) {
		return [][]byte{setClipboardPacket(text)}
	}
	chunks := chunkUTF8(text, maxInjectTextBytes)
	packets := make([][]byte, 0, len(chunks))
	for _, c := range chunks {
		packets = append(packets, injectTextPacket(c))
	}
	return packets
}

// writeText writes all packets for one text atomically with respect to other
// control messages (a tile session may be shared by several viewers).
func writeText(conn net.Conn, mu *sync.Mutex, text string) error {
	packets := textPackets(text)
	if len(packets) == 0 {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	for _, p := range packets {
		if _, err := conn.Write(p); err != nil {
			return err
		}
	}
	return nil
}

// streamScreenrecord is the fallback for devices that cannot start a modern
// scrcpy server. Video is capped at 30 fps by Android; input still goes through
// the HTTP endpoints.
func streamScreenrecord(dev string, ws *websocket.Conn, writeMu *sync.Mutex) {
	cmd := exec.Command(adbPath, adbFor(dev, "exec-out", "screenrecord",
		"--time-limit", "1800", "--output-format=h264", "--bit-rate", "12M", "-")...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()
	buf := make([]byte, 64<<10)
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			writeMu.Lock()
			e := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			writeMu.Unlock()
			if e != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
