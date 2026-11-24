# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Keyboard input actually reaches the phone now: clicking a grid tile (or the
  single-view video) routes physical keystrokes to that device only, and a new
  per-tile ⌨ text row sends whole strings. Backspace, Delete, Enter, Tab, the
  arrow keys and Escape release are mapped to Android key codes.
- Text over the scrcpy control channel: printable ASCII uses `INJECT_TEXT`;
  non-ASCII text uses `SET_CLIPBOARD` + `KEYCODE_PASTE` so Chinese and emoji
  work. The HTTP fallback keeps `adb shell input text` for ASCII and uses the
  ADB Keyboard IME broadcast for Unicode when that IME is selected.

### Fixed

- Typing on the physical keyboard while a tile was focused did nothing. The
  grid had no keyboard listener and the only text path was an ASCII-only
  `adb shell input text` call from single view.

## [0.1.1] - 2026-11-24

### Added

- Japanese (`ja`) and Vietnamese (`vi`) UI locales, with automatic browser
  detection and `?lang=ja` / `?lang=vi` overrides.

### Changed

- CI blocks on Linux and Windows. macOS moved to a manual workflow because
  hosted macOS runners are often unavailable; the release workflow still builds
  darwin/amd64 and darwin/arm64.

## [0.1.0] - 2026-11-23

### Added

- Multi-device grid and single-device view, both interactive.
- Direct `scrcpy-server` client: raw H.264 over WebSocket, WebCodecs decoding.
- Shared per-device tile session with keyframe cache and 30-second idle teardown.
- Touch, swipe, key and text input over the scrcpy control channel, with HTTP
  fallbacks for devices that cannot start a modern server.
- `screenrecord` fallback for old Android devices.
- Device auto-discovery, USB/wireless merge by `ro.serialno`, model names, rename.
- Loopback-by-default binding, mandatory token for LAN binds, POST-only mutations,
  same-origin WebSocket checks.
- English and Simplified Chinese UI with automatic browser detection, a manual
  language menu, and a `?lang=` override, embedded in a single binary.
- CI on Linux, macOS and Windows; cross-platform release builds.

[Unreleased]: https://github.com/TwitterIsGood/phone-wall/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/TwitterIsGood/phone-wall/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/TwitterIsGood/phone-wall/releases/tag/v0.1.0
