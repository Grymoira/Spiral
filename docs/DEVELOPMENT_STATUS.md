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
