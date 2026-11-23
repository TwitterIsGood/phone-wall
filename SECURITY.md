# Security

## Reporting a vulnerability

Please open a private security advisory on GitHub:
<https://github.com/TwitterIsGood/phone-wall/security/advisories/new>

Do not open a public issue for a vulnerability. You should get a reply within a
few days. Please include the version, a minimal reproduction, and the impact.

## Threat model

Phone Wall is a LAN tool that gives full input control of physical devices. It
assumes a trusted host and a trusted network.

- **Loopback by default.** Binding to anything other than a loopback address
  requires a token. The process refuses to start otherwise.
- **Shared token, no accounts.** The token is checked in constant time and can
  be sent as `Authorization: Bearer`, `X-Auth-Token`, a `pw_token` cookie, or a
  `?token=` query parameter. A browser that visits `?token=` receives an
  HttpOnly, SameSite=Strict cookie and is redirected to a clean URL.
- **No TLS.** The token and the video stream travel in clear text. Do not expose
  Phone Wall directly to the internet; use WireGuard, Tailscale, or an HTTPS
  reverse proxy.
- **Same-origin WebSockets.** The WebSocket upgrader rejects cross-origin
  browser handshakes. Mutating HTTP endpoints are POST-only; GET endpoints are
  read-only.
- **No command injection through text input.** Text typed in the UI is shell-
  quoted before it reaches `adb shell input text`. Even so, `adb input text`
  only supports ASCII and interprets `%s` as a space.
- **Secrets stay out of the repository.** `devices.json` and `names.json` are
  gitignored. Device serials, wireless-debugging addresses, and screenshots of
  device screens must never be committed.

## Out of scope

- Attacks that already require a shell on the host or physical access to an
  unlocked phone.
- Denial of service from a client that is already authorized and intentionally
  opens many sessions.
- Failures in `adb`, `scrcpy-server`, Android, or the browser.
