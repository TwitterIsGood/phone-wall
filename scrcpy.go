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

	"github.com/gorilla/websocket"
)

var (
	scrcpyJar     string
	scrcpyVersion string
)

// keycodes are Android key event codes understood by scrcpy's inject-keycode
// control message.
var keycodes = map[string]uint32{"back": 4, "home": 3, "recents": 187, "enter": 66}

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
	b[0] = 2
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
	b[0] = 0
	b[1] = action
	binary.BigEndian.PutUint32(b[2:], code)
	return b
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
