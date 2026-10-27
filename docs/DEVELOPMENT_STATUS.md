# Development status

Recorded: 2026-10-26. Repository: `Grymoira/Spiral`, default branch `main`.

## Verified repository facts at initial inspection

- Before this change, `main` contained only `README.md` (`# Spiral`, `螺线`) and the initial commit `03e35a8`.
- Historical SyncMusic Phase 0/1 reports were not present as code in Spiral; no existing Qt, Go, protocol, or test code could be reused.
- This change retains the original repository name and README heading, and adds a new, explicit phase-0 engineering baseline.

## Implemented in this increment

- CMake presets for Qt desktop or Qt-free Go baseline, and a Go build/CTest entry.
- Qt Quick local-file audio player experiment with file selection, play/pause/stop, seeking and basic error display. This has **not** been compiled or run here because the Qt SDK is unavailable.
- Go standard-library development HTTP server limited to local loopback addresses, with random room credentials, invitation authentication, 24-hour ephemeral tokens, five-seat atomic capacity, bounded 1024-room memory budget with expiration cleanup, room snapshots, and explicit host-leave room teardown.
- Unit tests for authorization, lifecycle, concurrent capacity, expiration, JSON validation, and HTTP behavior.
- GitHub Actions workflow for Go format, race tests, and compile; remote Actions run status must be checked separately.
- Introductory protocol, security constraints, engineering rules, roadmap and licensing status.

## Local verification evidence

Environment: Linux x86_64; Go 1.23.2; CMake 3.31.6; GNU C++ 14.2; Qt 6 SDK not found.

- `cd server && go test -race -count=1 ./...` — **PASS** for `internal/api` and `internal/rooms`; `cmd/spiral-server` has no test files.
- `cmake --preset server-only && cmake --build --preset server-only && ctest --preset server-only` — **PASS**; CTest executed 1 real Go test entry (0 failures).
- `cd server && go vet ./...` — **PASS** (no findings).

## Not yet delivered or verified

- Qt build, GUI runtime, media backend, and output timing on Windows/Linux/macOS.
- Public TLS/WSS, network rate limiting, persistent/resumable identities and secure cross-network session management.
- Server-authoritative playback state and timeline, clock-offset estimation, playback scheduling, readiness barrier, drift correction, and acoustic measurement.
- A connected two-client demo or an Internet-facing deployed service.
- Audio provider integration, cross-origin DNS/proxy behavior, music metadata, SQLite library, lyrics, or full Material Design 3.
- Overall project license selection; third-party sources have not been copied.

## Risk notes

The loopback HTTP API is a local functional experiment **only**. Avoid interpreting the existence of rooms as synchronous playback. Current 24-hour sessions are not resumable; they expire without renewal. Qt compilation and runtime are explicitly pending. The in-memory server must not be deployed publicly.

## Review and corrective increment — 2026-10-26

Source baseline: `main` commit `cd417715313e2a173da832a74fe3c94ab1a44b20`. Confirmed
that the local source files matched the remote Git blob hashes before editing, and
that GitHub Actions run 40345650523 for that earlier commit completed successfully.

### Corrections

- `CMakePresets.json`: version 6 (which requires a newer CMake) changed to version 5,
  consistent with the advertised CMake 3.24 minimum.
- `CMakeLists.txt`: the server-only configuration no longer requires a C++ compiler.
  Selected server builds now require Go at configuration time; disabling both
  components is an error, and Windows Go executables get a `.exe` suffix.
- Qt client CMake: explicitly require and link `QuickDialogs2`, which is imported by
  the QML file chooser. Qt compilation still requires a machine with Qt 6.8+ SDK.
- HTTP API: enforce one JSON object with unique top-level keys, UTF-8 JSON media
  type, bounded input, required nonempty join invitation; use HTTP 405 and `Allow`
  for unsupported methods on recognized resource paths.
- Server listener: check the **actual bound address** is loopback, not just the
  requested hostname. Still deliberately lacks TLS/WSS and is not remotely deployable.
- CI: add Go vet plus CMake/CTest checks; avoid writing an untracked binary into
  `server/` during CI builds.
- Tests: add negative listener tests, JSON/type/duplicate-key tests, HTTP method and
  security-header checks, and expired-guest credential/seat reuse regression coverage.

### Execution evidence (Linux x86_64, Go 1.23.2, CMake 3.31.6, Ninja 1.12.1)

- `cd server && go test -race -count=1 ./...` — **PASS** for all three packages
  (`cmd/spiral-server`, `internal/api`, `internal/rooms`); 13 top-level Go tests.
- `cd server && go vet ./...` and Go formatting check — **PASS**.
- `cmake --preset server-only && cmake --build --preset server-only && ctest --preset server-only --output-on-failure` — **PASS**; one CTest entry invoking Go test suite.
- Negative CMake checks — **PASS**: absence of Go via restricted PATH causes
  configuration failure; selecting zero components causes configuration failure.
- Live Go server smoke test — **PASS**: actual localhost process responded to health,
  room creation, invitation-based join, bearer-authorized snapshot, guest leave and
  final snapshot, without disclosing credential values to logs.

### Still unverified

- CMake 3.24 directly (local toolchain is 3.31.6), Windows/macOS compilation, and
  Qt 6.8+ QML module compilation/runtime because the Qt SDK is absent here.
- Real audio output and multi-device synchronization; WSS/clock/scheduling are still
  unimplemented. The new GitHub Actions workflow requires a new remote run before
  its results can be reported as successful.

## Server-coordinated synchronization implementation — 2026-10-27

User priority: real-time network connections and synchronized local playback. Based on `main` commit `b06fb319f0f6fcf99ccdfe4600c7374d7ae8f019` (the preceding reviewed baseline). No existing repository files were discarded.

Implemented:

- Go RFC 6455 text WebSocket with 4 KiB frame/message bound, required client masking, limited accepted opcodes, control ping/pong, origin check, and message rate limiting. Initial authenticated hello uses token in the first frame (not URL). HTTPS/WSS listener can bind to a public address only with a server certificate/key; without TLS the server binds exclusively to loopback.
- Bound room authority: member-ready state, 64-hex SHA-256 recording identity, per-track generation, server clock epoch, monotonic timeline, room state revision, and a replaceable future action with ID and deadline. Old scheduled events and old-track confirmations cannot commit.
- Readiness barrier before play, host-only track/schedule control, authorization on every inbound WS command, state broadcast, room snapshot and client reconnect handling. Disconnected members lose readiness.
- Qt Quick client source: HTTP(S) creation and join, authenticated WS/WSS, local file hashing, local media-load check, four-timestamp offset and jitter filtering, future action timer, pause/seek/play authority reconciliation, large-error corrective seeks, pause-on-disconnect and session re-auth.
- Full first-party `LICENSE` with MIT text. Dependency distribution notices are separately documented.

Verification:

- Go `go test -race -count=1 ./...` including two genuine independently connected TCP WebSocket clients — **PASS** on local Linux Go 1.23.2.
- Go `go vet ./...`, `go test -race -count=2 ./...`, `go build -o /mnt/data/spiral-server-smoke ./cmd/spiral-server`, `cmake --preset server-only`, `cmake --build --preset server-only`, `ctest --preset server-only --output-on-failure` — **PASS** (Linux x86_64, Go 1.23.2, CMake 3.31.6; CTest 1/1).
- **Not executed:** Qt 6.8+ build or QML runtime (Qt SDK absent), two physical devices, TLS on a public Internet host, real acoustic alignment or sample-level latency/error measurements.

**Milestone interpretation:** this is a practical first slice of Phase 2/3 (synchronization protocol and two-socket transport), not completion of Phase 2/3 acceptance. The earlier `docs/adr/0001-loopback-bootstrap.md` remains a historical account of the original HTTP-only baseline; see `docs/adr/0002-coordinated-realtime-sync.md` for the new transport direction.

## Stability and observability follow-up — 2026-10-27

Baseline: `main` commit `0978f248e7ccdd5ea4ee559e2f22363a79427a2d`.
A remote GitHub Actions run (40364849956) had completed successfully,
including the Linux Qt 6.8.3 desktop **compile** and Go tests. That is not
proof of end-to-end audio output or production readiness.

Corrections implemented in this increment:

- Cancel scheduled playing-state transitions when any member revokes Ready or
  when a newly joining member has not prepared the current recording; repeat
  the readiness check at the event commit deadline. Preserve safe pending pause
  actions. Disconnects invalidate readiness and cancel pending play.
- Enforce a global and per-client-IP concurrent WebSocket capacity budget to
  reduce unauthenticated connection exhaustion, and reject frames from a
  connection superseded by a later login of the same member.
- Add JSONL event logging to the server and a bounded, rotated, allowlisted
  local event journal to the desktop client. Fixed route/event codes do not
  include tokens, invites, media paths, raw URLs, request bodies or messages.
  See `docs/LOGGING.md` for schema, correlation and retention behavior.
- Extend CI to attempt launching the compiled Qt QML desktop in a virtual
  display, rather than treating compilation alone as a runtime smoke test.
- Add regression tests for readiness cancellation, stale timers, concurrent
  WebSocket admission, superseded-session fencing and log schema/redaction.

Local Linux verification (Go 1.23.2) for this change:

- `go vet ./...` — PASS.
- `go test -race -count=2 ./...` — PASS.
- Go tests include real network WebSocket integration tests and expiry logic.
- Server-only CMake and CTest results and Qt CI outcome are reported separately
  when actually run; do not infer acoustic accuracy from either test suite.

**Not delivered:** Full ARM/ACK/COMMIT two-phase playback agreement; physical
multi-device acoustic output measurement; durable rate limits, WAF/reverse
proxy review and comprehensive public-host attack testing; chat/danmaku;
macOS/Windows end-user installers. Under network partitioning a previously
received scheduled command can still be executed by a client before it sees a
cancellation; the system therefore does not guarantee atomic simultaneous
playback. This is not a final Release candidate.

## Client transport policy hardening — 2026-10-27

Issue #14: before creating/joining a room, Spiral now validates the server
origin using Qt's strict URL parser. Plaintext HTTP/WS is accepted only for
explicit loopback IP addresses or `localhost`; remote servers require
HTTPS/WSS. Userinfo, queries, fragments and non-root paths are rejected.
The client constructs WSS/WS URLs only from the validated origin and applies
the same check before sending an authenticated leave request.

The QML network manager uses a same-origin redirect policy, so an accepted
server cannot redirect a membership request to another origin or downgrade
from HTTPS to HTTP. This does **not** replace TLS certificate validation,
public-host abuse limits, or end-to-end threat modeling.

The Qt desktop CI now runs transport URL and redirect-policy tests, alongside
existing Qt/QML and Go checks. Real public-host interception and cross-platform
package validation remain outstanding.

## Anonymous room admission guard — 2026-10-27

Issue #10: public `POST /v1/rooms` creation now uses the actual HTTP
peer address, canonicalizing IPv4-mapped IPv6 and ignoring untrusted
`X-Forwarded-For`/`Forwarded` headers. The Store atomically enforces:

- 24 simultaneous rooms created per source IP;
- at most 6 accepted creations per source per rolling minute;
- at most 48 accepted public creations globally per rolling minute;
- a 15-minute lease for rooms with no authenticated access or successful join.

An authenticated WebSocket, authorized snapshot, or valid join claims the
room, preserving the existing 24-hour member session lifetime. A rejected
creation does not consume quota; source and global counters use only a
bounded rolling record of successful creations (no unbounded per-IP maps).
429 errors distinguish source capacity from creation rate, with a one-minute
Retry-After hint for the latter. Existing join/leave/playback/reconnect
endpoints are not rate-limited by this change.

**Deployment boundary:** one source means the direct TCP peer IP, not a
trusted human identity. Shared NAT users and reverse proxies share a
budget; IPv6 address rotation, botnets, sybil clients and distributed
attacks still require authenticated creation and edge-level limits.
Do not trust forwarded client-IP headers without an explicit trusted-proxy
configuration. Time-based cleanup runs on room operations and creation;
it is not a background sweep.

## Strict real-time message contract — 2026-10-27

Issue #6: the WebSocket envelope parser now requires every field defined by
the individual command schema to be present and non-\`null\`. Previously,
missing or \`null\` JSON scalars decoded to Go zero values (notably
\`ready:false\` and \`mediaMs:0\`), potentially mutating authoritative room
state. Exact field names, duplicate detection, type checking, and
command-specific allowed fields continue to use the existing parser;
valid \`ready:false\`, \`mediaMs:0\`, and \`c0:0\` retain their original meaning.

Regression tests enumerate missing/null for every required field, malformed
types, unexpected/duplicated fields and valid zero values. A real two-way
WebSocket test verifies rejected packets return \`invalid_message\` without
changing state revision, readiness or pending playback.
