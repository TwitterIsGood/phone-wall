package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type wsMsg struct {
	A  string  `json:"a"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	FW float64 `json:"fw"`
	FH float64 `json:"fh"`
	K  string  `json:"k"`
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("dev")
	if _, ok := findDevice(key); !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	dev := activeSerial(key)
	if dev == "" {
		http.Error(w, "device offline", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Query().Get("tile") == "1" {
		handleTileWS(w, r, key, dev)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	var writeMu sync.Mutex
	sendText := func(v any) {
		b, _ := json.Marshal(v)
		writeMu.Lock()
		_ = ws.WriteMessage(websocket.TextMessage, b)
		writeMu.Unlock()
	}

	sess, err := startSession(key, dev, false)
	if err != nil {
		log.Printf("scrcpy %s: %v (falling back to screenrecord)", key, err)
		sendText(map[string]any{"mode": "screenrecord", "error": err.Error()})
		go func() {
			streamScreenrecord(dev, ws, &writeMu)
			ws.Close()
		}()
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			// Input in fallback mode goes through the HTTP endpoints.
		}
	}
	defer sess.close()
	sendText(map[string]any{"ready": true})

	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := sess.video.Read(buf)
			if n > 0 {
				writeMu.Lock()
				e := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
				writeMu.Unlock()
				if e != nil {
					ws.Close()
					return
				}
			}
			if err != nil {
				ws.Close()
				return
			}
		}
	}()

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
			x, y, sw, sh := scaleToDevice(key, dev, m.X, m.Y, m.FW, m.FH)
			act := byte(0)
			if m.A == "move" {
				act = 2
			} else if m.A == "up" {
				act = 1
			}
			_, _ = sess.control.Write(touchPacket(act, x, y, sw, sh))
		case "key":
			if kc, ok := keycodes[m.K]; ok {
				_, _ = sess.control.Write(keyPacket(0, kc))
				_, _ = sess.control.Write(keyPacket(1, kc))
			}
		}
	}
}
