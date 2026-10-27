# Spiral · 螺线

A cross-platform experimental **multi-device synchronized local-audio player**. Every participant independently chooses the *same audio file*; the Go server coordinates authenticated rooms, clocks and scheduled playback commands. **Audio bytes are not relayed by the server.**

> **Status: experimental development milestone, not a stable release.** Two independent WebSocket clients have been tested against the synchronization server. The Qt desktop client **compiles and passes headless startup smoke tests on Linux Qt 6.8.3 CI**; real interactive device playback and multi-device acoustic sync remain unverified. A Windows x64 portable test-preview packaging workflow is available, but does not prove physical accuracy. This is server-coordinated client-to-server networking, not device-to-device P2P NAT traversal.

## Implemented now

- HTTPS/WSS coordinator with host/member authentication, max 5 seats, room snapshot, session expiration and disconnect readiness revocation; plaintext HTTP/WS restricted to loopback.
- Server-authoritative track SHA-256, generation and playback revision, four-timestamp monotonic clock probes, readiness barrier, play/pause/seek scheduling at the same future server time, stale-event protection and state snapshots for reconnect.
- Qt Quick desktop source using `Qt Multimedia` and `Qt WebSockets`: create/join room, local file SHA-256 check, ready status, clock estimate, host controls, scheduled playback and conservative drift correction.
- Go unit/integration tests covering two real WebSocket TCP connections, permissions, stale events and shared scheduled timeline. **No test here measures the audible output of two devices.**

## Requirements

- Server: Go 1.23+ (no third-party Go modules).
- Desktop: Qt 6.8+ with Quick, Quick Controls, Quick Dialogs, Multimedia, WebSockets, C++20 compiler and CMake >= 3.24.

## Build & test

```sh
# Server standalone
cd server
go test -race ./...
go build -o spiral-server ./cmd/spiral-server
./spiral-server -listen 127.0.0.1:8787

# From the project root: Qt SDK required for 'dev'
cmake --preset dev
cmake --build --preset dev

# Without Qt SDK
cmake --preset server-only
cmake --build --preset server-only
ctest --preset server-only
```

Launch `spiral-client` after building. The default server address is `http://127.0.0.1:8787`; this address works **only on the same machine**. To test two clients on distinct networks, provide a publicly reachable TLS endpoint and enter the same `https://hostname:port` address in both clients.

## Running across different networks (experimental)

1. Deploy the server on a publicly reachable VPS or protected gateway. Point DNS to it; allow inbound TCP on the chosen port. **Do not expose the plaintext loopback mode to the Internet.**
2. Install a *trusted, valid* TLS certificate/key for your hostname, then run:

   ```sh
   ./spiral-server -listen 0.0.0.0:8787 -tls-cert /path/to/fullchain.pem -tls-key /path/to/privkey.pem
   ```

3. On each machine set the server field to `https://your-hostname.example:8787`. The application uses `wss://` for live room messages. The host selects a local test recording, creates a room and securely shares the room ID plus invitation code with other members. Each member independently opens the **identical file**.
4. The host clicks **Host: share track** after it is loaded. Wait for every member to show *Ready* and for the time estimate to be calibrated, then click **Play together**. Only the host can issue room playback commands; guest local playback controls do not affect room state.

The HTTPS/WSS path is available but **not production hardened** (no credential renewal, anti-abuse suite, secure keychain, reverse-proxy deployment guide or real Internet soak tests). Do not use a self-signed certificate without a properly configured OS trust store, and do not disable certificate verification.

## Limitations and next work

- Audio timing uses Qt player-reported media position; no sound-card output-latency compensation or actual cross-device acoustic error measurements yet.
- Clock offset uses four timestamps and an RTT filter; asymmetric network latency remains a source of systematic error. Inconsistent clocks/large delays can cause degraded synchronization.
- Readiness is a client assertion based on file hash and local media-load state, not a server-side proof of decodability or identical audio output.
- Event-scoped ARM/ACK/COMMIT and cancellation receipts are implemented at the application layer, but these are **not physical acoustic output confirmations**. There is no proven simultaneous acoustic onset, fine-grained rate control, durable rooms, track queues, network music providers, NAT-traversing peer-to-peer sockets, or public deployment hardening. The server holds no music files or platform credentials.
- Interactive Qt GUI and real sound measurements on Windows, Linux and macOS remain to be verified. Only a **clearly labeled Windows x64 technical test preview** is planned; do not treat that build as a signed/stable release or as Issue #11/#12 final acceptance.

## Documentation

- [Protocol and timing semantics](protocol/README.md)
- [Development status and test evidence](docs/DEVELOPMENT_STATUS.md)
- [Roadmap](docs/roadmap.md)
- [Local bootstrap decision](docs/adr/0001-loopback-bootstrap.md)
- [MIT License](LICENSE) and [third-party licensing notes](docs/LICENSING.md)
- [Windows x64 technical preview and local test procedure](docs/PREVIEW_RELEASE_v0.1.0-preview.1.md)

## Structured event logs

The Go server emits a UTF-8 JSON object per line to stdout, with UTC timestamp,
severity, stable event code, component, request/connection correlation ID and
relevant numeric status/timing fields. Adjust verbosity with `-log-level warn`
(or `debug`, `info`, `error`). The Qt client writes bounded, locally rotated
`events.jsonl` records to its application data directory. Neither channel is
intended to log room invitations, bearer tokens, arbitrary URLs or audio paths.
See [Logging contract](docs/LOGGING.md).

**Engineering status:** Readiness cancellation, per-IP/global WebSocket limits,
reconnect fencing and strict log redaction have automated tests. The Linux Qt
client previously passed compilation in CI, but this increment also adds a
headless QML *startup* smoke test. Acoustic synchrony on physical devices is
not verified. Chat/danmaku remain intentionally deferred, and public-host
security hardening is still incomplete. Do not treat this as a production release.
