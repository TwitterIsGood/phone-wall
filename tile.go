package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// A tile session is shared by every viewer of the same device. It survives a
// page refresh (30 s idle grace) and keeps the latest GOP cached, so a new
// WebSocket receives a keyframe immediately instead of waiting for the encoder.
type tileSub struct {
	ws *websocket.Conn
	mu sync.Mutex
}

type tileSession struct {
	key, dev string
	once     sync.Once
	err      error

	mu      sync.Mutex
	subs    map[*tileSub]bool
	cache   []byte // SPS/PPS + latest IDR + following NALs
	sps     []byte
	pps     []byte
	closed  bool
	stopAt  *time.Timer
	ctrlMu  sync.Mutex
	video   net.Conn
	control net.Conn
	cmd     *exec.Cmd
	port    int
	stop    chan struct{}
}

var (
	tileMu       sync.Mutex
	tileSessions = map[string]*tileSession{}
)

func getTileSession(key, dev string) (*tileSession, error) {
	tileMu.Lock()
	ts := tileSessions[key]
	if ts == nil {
		ts = &tileSession{key: key, dev: dev, subs: map[*tileSub]bool{}, stop: make(chan struct{})}
		tileSessions[key] = ts
	}
	tileMu.Unlock()
	ts.once.Do(func() { ts.err = ts.start() })
	if ts.err != nil {
		tileMu.Lock()
		if tileSessions[key] == ts {
			delete(tileSessions, key)
		}
		tileMu.Unlock()
		return nil, ts.err
	}
	return ts, nil
}

func (ts *tileSession) start() error {
	s, err := startSession(ts.key, ts.dev, true)
	if err != nil {
		return err
	}
	ts.video = s.video
	ts.control = s.control
	ts.cmd = s.cmd
	ts.port = s.port
	go ts.videoPump()
	return nil
}

func (ts *tileSession) addSub(ws *websocket.Conn) *tileSub {
	sub := &tileSub{ws: ws}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		return nil
	}
	ts.subs[sub] = true
	if ts.stopAt != nil {
		ts.stopAt.Stop()
		ts.stopAt = nil
	}
	if len(ts.cache) > 0 {
		sub.mu.Lock()
		_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = ws.WriteMessage(websocket.BinaryMessage, ts.cache)
		sub.mu.Unlock()
	}
	return sub
}

func (ts *tileSession) removeSub(sub *tileSub) {
	ts.mu.Lock()
	delete(ts.subs, sub)
	if len(ts.subs) == 0 && !ts.closed {
		ts.stopAt = time.AfterFunc(30*time.Second, ts.stopIfIdle)
	}
	ts.mu.Unlock()
}

func (ts *tileSession) stopIfIdle() {
	ts.mu.Lock()
	if ts.closed || len(ts.subs) > 0 {
		ts.mu.Unlock()
		return
	}
	ts.closed = true
	close(ts.stop)
	ts.mu.Unlock()
	ts.teardown()
}

func (ts *tileSession) teardown() {
	if ts.video != nil {
		ts.video.Close()
	}
	if ts.control != nil {
		ts.control.Close()
	}
	if ts.cmd != nil && ts.cmd.Process != nil {
		ts.cmd.Process.Kill()
	}
	removeForward(ts.dev, ts.port)
	tileMu.Lock()
	if tileSessions[ts.key] == ts {
		delete(tileSessions, ts.key)
	}
	tileMu.Unlock()
}

func (ts *tileSession) isClosed() bool {
	select {
	case <-ts.stop:
		return true
	default:
		return false
	}
}

func nalHeader(b []byte) int {
	if len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1 {
		return 4
	}
	if len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1 {
		return 3
	}
	return -1
}

// splitNALs returns complete Annex-B NAL units and the trailing (possibly
// incomplete) one.
func splitNALs(buf []byte) (nals [][]byte, rest []byte) {
	starts := make([]int, 0, 8)
	for i := 0; i+3 <= len(buf); i++ {
		if buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 1 {
			starts = append(starts, i)
			i += 2
			continue
		}
		if i+4 <= len(buf) && buf[i] == 0 && buf[i+1] == 0 && buf[i+2] == 0 && buf[i+3] == 1 {
			starts = append(starts, i)
			i += 3
		}
	}
	if len(starts) == 0 {
		if len(buf) > 4 {
			return nil, buf[len(buf)-4:]
		}
		return nil, buf
	}
	for j := 0; j+1 < len(starts); j++ {
		s, e := starts[j], starts[j+1]
		for e > s && buf[e-1] == 0 {
			e--
		}
		if e > s {
			nals = append(nals, buf[s:e])
		}
	}
	return nals, buf[starts[len(starts)-1]:]
}

func (ts *tileSession) broadcast(nals [][]byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		return
	}
	for _, nal := range nals {
		h := nalHeader(nal)
		if h < 0 || h >= len(nal) {
			continue
		}
		switch typ := nal[h] & 0x1f; typ {
		case 7:
			ts.sps = append([]byte(nil), nal...)
		case 8:
			ts.pps = append([]byte(nil), nal...)
		case 5:
			ts.cache = ts.cache[:0]
			ts.cache = append(ts.cache, ts.sps...)
			ts.cache = append(ts.cache, ts.pps...)
			ts.cache = append(ts.cache, nal...)
		default:
			if len(ts.cache) > 0 {
				ts.cache = append(ts.cache, nal...)
			}
		}
		if len(ts.cache) > 8<<20 {
			ts.cache = ts.cache[:0]
		}
		for sub := range ts.subs {
			sub.mu.Lock()
			_ = sub.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := sub.ws.WriteMessage(websocket.BinaryMessage, nal)
			sub.mu.Unlock()
			if err != nil {
				sub.ws.Close()
			}
		}
	}
}

func (ts *tileSession) videoPump() {
	var buf []byte
	tmp := make([]byte, 64<<10)
	for {
		if ts.isClosed() {
			return
		}
		ts.video.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		n, err := ts.video.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			nals, rest := splitNALs(buf)
			buf = rest
			if len(nals) > 0 {
				ts.broadcast(nals)
			}
		}
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if len(buf) > 0 {
					ts.broadcast([][]byte{buf})
					buf = nil
				}
				continue
			}
			ts.stopIfIdleForever()
			return
		}
	}
}

// stopIfIdleForever tears the session down when the video stream dies, even if
// viewers are still attached; their next reconnect starts a fresh session.
func (ts *tileSession) stopIfIdleForever() {
	ts.mu.Lock()
	if ts.closed {
		ts.mu.Unlock()
		return
	}
	ts.closed = true
	close(ts.stop)
	ts.mu.Unlock()
	ts.teardown()
}

func handleTileWS(w http.ResponseWriter, r *http.Request, key, dev string) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	ts, serr := getTileSession(key, dev)
	if serr != nil {
		log.Printf("TILE %s session failed: %v (falling back to screenrecord)", key, serr)
		var wm sync.Mutex
		b, _ := json.Marshal(map[string]any{"mode": "screenrecord", "error": serr.Error()})
		wm.Lock()
		_ = ws.WriteMessage(websocket.TextMessage, b)
		wm.Unlock()
		streamScreenrecord(dev, ws, &wm)
		return
	}

	sub := ts.addSub(ws)
	if sub == nil {
		return
	}
	log.Printf("TILE %s subscribed cache=%d", key, len(ts.cache))
	defer ts.removeSub(sub)
	ready, _ := json.Marshal(map[string]any{"ready": true})
	sub.mu.Lock()
	_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = ws.WriteMessage(websocket.TextMessage, ready)
	sub.mu.Unlock()

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var m wsMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.A {
		case "down", "move", "up":
			// The tile stream is scaled by max_size; send frame coordinates
			// and let the scrcpy server map them to the display.
			x, y := int(m.X+0.5), int(m.Y+0.5)
			sw, sh := int(m.FW+0.5), int(m.FH+0.5)
			act := byte(0)
			if m.A == "move" {
				act = 2
			} else if m.A == "up" {
				act = 1
			}
			ts.ctrlMu.Lock()
			_, werr := ts.control.Write(touchPacket(act, x, y, sw, sh))
			ts.ctrlMu.Unlock()
			if werr != nil {
				log.Printf("TILE %s control write: %v", key, werr)
			}
		case "key":
			if kc, ok := keycodes[m.K]; ok {
				ts.ctrlMu.Lock()
				_, _ = ts.control.Write(keyPacket(0, kc))
				_, _ = ts.control.Write(keyPacket(1, kc))
				ts.ctrlMu.Unlock()
			}
		case "text":
			if err := writeText(ts.control, &ts.ctrlMu, m.T); err != nil {
				log.Printf("TILE %s text: %v", key, err)
			}
		}
	}
}
