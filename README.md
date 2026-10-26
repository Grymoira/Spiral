# Spiral

螺线 — an open-source, cross-platform music synchronization project.

> **Status: early engineering baseline, not a production-ready synchronized player.**
> The Qt client currently plays local files only. The Go service currently manages ephemeral rooms over a *loopback-only* development HTTP API. The client is not yet connected to the service, and no end-to-end or acoustic synchronization has been verified.

## Intended system

- **Desktop client:** C++20, Qt 6.8+, Qt Quick/QML, Qt Multimedia.
- **Coordination server:** Go 1.23+; future authenticated WSS for room state, clock probes, and scheduling. The server must not relay audio.
- **Music sources:** begin with identical authorized local test files; add licensed network sources only after provider/security review.
- **Rooms:** maximum five members including the host; the server is authoritative for membership and, in later milestones, playback state.
- **Platforms:** Windows, Linux, macOS (subject to toolchain and real-device testing).

## Build and test

Requirements: CMake >= 3.24, Ninja, Go >= 1.23. To build the desktop client, also install a Qt 6.8+ SDK with Quick, Quick Controls, Multimedia, and Qt Quick Dialogs.

```sh
# No Qt SDK required for the Go baseline:
cmake --preset server-only
cmake --build --preset server-only
ctest --preset server-only
cd server && go test -race -count=1 ./...

# With a compatible Qt 6 SDK installed and discoverable by CMake:
cmake --preset dev
cmake --build --preset dev
```

Run the development server:

```sh
cd server
go run ./cmd/spiral-server -listen 127.0.0.1:8787
```

Example local-only room creation (credentials are secrets; never paste them into issue reports or logs):

```sh
curl -sS -X POST http://127.0.0.1:8787/v1/rooms \
  -H 'Content-Type: application/json' -d '{}'
```

The response contains the host session token and invitation code. See [`protocol/README.md`](protocol/README.md) for supported prototype operations. **Do not bind this plaintext development server to public interfaces or use it as an Internet-facing service.**

## Project documents

- [Roadmap](docs/roadmap.md)
- [Actual development status](docs/DEVELOPMENT_STATUS.md)
- [Engineering guidelines](AGENTS.md)
- [Licensing status](docs/LICENSING.md)
- [Temporary transport decision](docs/adr/0001-loopback-bootstrap.md)

No published downloads or release tags yet. See development status for what has actually been tested.
