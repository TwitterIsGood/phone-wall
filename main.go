// phone-wall is a self-hosted wall of physical Android devices in the browser.
// It talks to scrcpy-server directly, streams H.264 over WebSocket and decodes
// it with WebCodecs. No desktop client, no front-end build step.
package main

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed web.html
var webHTML []byte

//go:embed grid.html
var gridHTML []byte

//go:embed i18n.js
var i18nJS []byte

var version = "dev"

var (
	flagHost     = flag.String("host", "127.0.0.1", "listen address (0.0.0.0 exposes the wall to your LAN and requires --token)")
	flagPort     = flag.Int("port", 8765, "listen port")
	flagConfig   = flag.String("config", "devices.json", "device list file; when missing, devices are discovered with adb")
	flagNames    = flag.String("names", "names.json", "file where the rename button stores display names")
	flagToken    = flag.String("token", envOr("PHONE_WALL_TOKEN", ""), "shared access token, also read from PHONE_WALL_TOKEN")
	flagADB      = flag.String("adb", envOr("ADB", "adb"), "path to the adb executable")
	flagJar      = flag.String("scrcpy-jar", envOr("SCRCPY_JAR", ""), "path to scrcpy-server; auto-detected when empty")
	flagJarVer   = flag.String("scrcpy-version", envOr("SCRCPY_VERSION", "5.0.1"), "scrcpy-server version")
	flagCleanFwd = flag.Bool("clean-forwards", true, "remove stale scrcpy adb forwards on startup")
	flagShowVer  = flag.Bool("version", false, "print the version and exit")
)

var token string

var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
	CheckOrigin:     sameOrigin,
}

func main() {
	flag.Parse()
	if *flagShowVer {
		fmt.Printf("phone-wall %s\n", version)
		return
	}
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(64 << 20)

	adbPath = *flagADB
	scrcpyVersion = *flagJarVer
	scrcpyJar = *flagJar
	token = *flagToken

	if err := validateListen(*flagHost, token); err != nil {
		log.Fatal(err)
	}
	if err := loadDevices(*flagConfig); err != nil {
		log.Fatalf("devices: %v", err)
	}
	loadNames(*flagNames)
	if *flagCleanFwd {
		cleanForwards()
	}
	go statusWorker()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", htmlHandler(webHTML))
	mux.HandleFunc("GET /grid", htmlHandler(gridHTML))
	mux.HandleFunc("GET /i18n.js", jsHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /status", statusHandler)
	mux.HandleFunc("GET /devices", devicesHandler)
	mux.HandleFunc("GET /frame/{key}", frameHandler)
	mux.HandleFunc("GET /ws", wsHandler)
	mux.HandleFunc("POST /rename/{key}", renameHandler)
	mux.HandleFunc("POST /tap/{key}", tapHandler)
	mux.HandleFunc("POST /swipe/{key}", swipeHandler)
	mux.HandleFunc("POST /key/{key}", keyHandler)
	mux.HandleFunc("POST /text/{key}", textHandler)

	srv := &http.Server{
		Addr:              net.JoinHostPort(*flagHost, fmt.Sprint(*flagPort)),
		Handler:           withAuth(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("bind %s: %v", srv.Addr, err)
	}
	scheme := "http"
	log.Printf("phone-wall %s listening on %s://%s/", version, scheme, ln.Addr())
	if token == "" {
		log.Printf("no token set; only loopback clients can reach the wall")
	}
	if err := srv.Serve(ln); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateListen(host, tok string) error {
	if isLoopbackHost(host) || tok != "" {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q without a token: pass --token (or PHONE_WALL_TOKEN) or bind to 127.0.0.1", host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func htmlHandler(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	}
}

func jsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(i18nJS)
}

func jsonReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

// withAuth protects every route when a token is configured. The token can be
// sent as a bearer token, an X-Auth-Token header, a cookie set by an earlier
// ?token= visit, or a ?token= query parameter.
func withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !authorized(r) {
			http.Error(w, "unauthorized: add ?token=... or send X-Auth-Token", http.StatusUnauthorized)
			return
		}
		// A browser that arrives with ?token= gets a cookie and a clean URL.
		// WebSocket clients keep their query string (they cannot follow a
		// redirect), and POST keeps it too so the method is preserved.
		if r.Method == http.MethodGet && !websocket.IsWebSocketUpgrade(r) && r.URL.Query().Get("token") != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     "pw_token",
				Value:    token,
				Path:     "/",
				MaxAge:   30 * 24 * 3600,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			q := r.URL.Query()
			q.Del("token")
			u := *r.URL
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.String(), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request) bool {
	if token == "" {
		return true
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) && tokenEqual(strings.TrimPrefix(h, prefix)) {
		return true
	}
	if tokenEqual(r.Header.Get("X-Auth-Token")) {
		return true
	}
	if c, err := r.Cookie("pw_token"); err == nil && tokenEqual(c.Value) {
		return true
	}
	return tokenEqual(r.URL.Query().Get("token"))
}

func tokenEqual(v string) bool {
	return subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser client
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
