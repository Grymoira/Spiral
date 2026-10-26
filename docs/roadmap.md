# Roadmap — Spiral

The original SyncMusic research described Phase 0–7, but no corresponding source files existed in this Spiral repository at initialization. These are the milestone mappings for work going forward; completing a source file is not equivalent to validating the milestone.

- [ ] **Phase 0 / Milestone A — reproducible baseline.** Go room core and HTTP development server, tests, initial Qt local audio player and CMake/CI scaffolding are implemented. Go baseline tested on Linux. Qt client build and runtime, Windows/macOS builds, and audio test remain unverified. A follow-up review hardened loopback binding, JSON validation, method responses and CMake missing-toolchain behavior; local Go tests and the Linux server-only build pass.
- [ ] **Phase 1 / Milestone B — audio capability experiment.** Measure local playback, seek, actual playback rate support, buffer and output timing; document backend limitations with test audio.
- [ ] **Phase 2 / Milestone C — sync protocol v1.** Finalize authenticated WSS, versioned contract, room authority, track identity, monotonic clocks, readiness barrier, scheduling and error semantics; executable Go/C++ contract tests.
- [ ] **Phase 3 / Milestone D — two-client synchronization.** Connect two client instances, load identical verified test audio, measure scheduling/drift, handle pause/seek/reconnect; then real devices and audio measurement.
- [ ] **Phase 4 / Milestone E — licensed network providers and proxy.** Client-side authorized media access, stream support, DNS/TLS/HTTP/WSS proxy path verification.
- [ ] **Phase 5 / Milestone F — usability and local library.** SQLite indexing, provider interfaces, room UX, lyrics, diagnostics, theme and accessibility.
- [ ] **Phase 6–7 / Milestone G — hardening and release.** Security, latency measurements, endurance testing, CI platform matrix, licensing and packaging.

**Next task:** install/find Qt 6.8+ (including Multimedia and Quick Dialogs), compile the local player, test local WAV/FLAC/MP3 play/pause/seek, and record timing and errors; then prepare the WSS protocol contract. No acoustic latency claim is currently supported.

## 2026-10-27: network/synchronization proof-of-concept

- [x] Implement a real Go WS endpoint with authenticated membership and authoritative state broadcasts.
- [x] Implement a server-side monotonic clock probe, SHA-256 track identity, readiness barrier and scheduled play/pause/seek commands.
- [x] Implement Qt Quick source connecting to the coordinator with local file verification, clock estimates and scheduled playback (not yet compiled).
- [x] Test two independent WebSocket clients against one server, checking shared event deadline, access control and state transitions.
- [x] Add the first-party MIT license (third-party dependencies retain their own licenses).
- [ ] **Next required acceptance:** compile/run Qt client with Qt 6.8+ on a supported OS, fix any QML/runtime errors, and perform two physical-device audio capture tests with quantified P50/P95/max alignment error.
- [ ] Add authenticated WSS deployment soak tests, reconnection/clock regression tests, bounded state and per-IP abuse controls, and end-user packaging.
