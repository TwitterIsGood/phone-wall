# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/TwitterIsGood/phone-wall/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/TwitterIsGood/phone-wall/releases/tag/v0.1.0
