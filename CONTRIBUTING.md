# Contributing

Thanks for taking a look. Phone Wall is small on purpose: a Go file set plus two
embedded HTML files, no front-end build step.

## Development

```bash
go run . --scrcpy-jar /path/to/scrcpy-server
go vet ./...
go test ./...
gofmt -l .              # must print nothing
node tools/check-i18n.js
```

To exercise the UI you need at least one Android device (or emulator) visible to
`adb devices`, and `scrcpy-server` 5.x. The device-side jar is never committed.

## Guidelines

- Keep the dependency count at one (`gorilla/websocket`). Prefer the standard
  library and the browser platform.
- UI strings live in `i18n.js` (`en`, `zh-CN`, `ja`, `vi`). Add a key to
  **every** locale, then use it from HTML with `data-i18n`,
  `data-i18n-placeholder` or `data-i18n-title`, or from JS with
  `I18N.t("key", {name: value})`. `node tools/check-i18n.js` fails on a missing
  key or an empty value.
- Mutating endpoints are POST. Input sent to the device must be shell-quoted.
- Do not commit `devices.json`, `names.json`, device serials, LAN addresses,
  tokens, or screenshots that show a device's real screen. Use documentation
  ranges such as `192.0.2.0/24` in examples.
- Keep the two HTML files free of external requests; everything must work from a
  single binary on a disconnected LAN.
- New behaviour should come with a test where it is testable without a device
  (protocol parsing, packet layout, config validation, auth).

## Commit messages

Conventional Commits are welcome, but plain descriptive messages are fine.

```
feat: cache the latest GOP per device
fix: keep the scrcpy session alive when a tab reconnects
docs: explain the token flow
```

## Pull requests

Fill in the PR template, keep the diff focused, and make sure CI is green on
Linux, macOS and Windows.
