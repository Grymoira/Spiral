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
