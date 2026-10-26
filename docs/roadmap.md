# Roadmap — Spiral

The original SyncMusic research described Phase 0–7, but no corresponding source files existed in this Spiral repository at initialization. These are the milestone mappings for work going forward; completing a source file is not equivalent to validating the milestone.

- [ ] **Phase 0 / Milestone A — reproducible baseline.** Go room core and HTTP development server, tests, initial Qt local audio player and CMake/CI scaffolding are implemented. Go baseline tested on Linux. Qt client build and runtime, Windows/macOS builds, and audio test remain unverified.
- [ ] **Phase 1 / Milestone B — audio capability experiment.** Measure local playback, seek, actual playback rate support, buffer and output timing; document backend limitations with test audio.
- [ ] **Phase 2 / Milestone C — sync protocol v1.** Finalize authenticated WSS, versioned contract, room authority, track identity, monotonic clocks, readiness barrier, scheduling and error semantics; executable Go/C++ contract tests.
- [ ] **Phase 3 / Milestone D — two-client synchronization.** Connect two client instances, load identical verified test audio, measure scheduling/drift, handle pause/seek/reconnect; then real devices and audio measurement.
- [ ] **Phase 4 / Milestone E — licensed network providers and proxy.** Client-side authorized media access, stream support, DNS/TLS/HTTP/WSS proxy path verification.
- [ ] **Phase 5 / Milestone F — usability and local library.** SQLite indexing, provider interfaces, room UX, lyrics, diagnostics, theme and accessibility.
- [ ] **Phase 6–7 / Milestone G — hardening and release.** Security, latency measurements, endurance testing, CI platform matrix, licensing and packaging.

**Next task:** install/find Qt 6.8+ (including Multimedia and Quick Dialogs), compile the local player, test local WAV/FLAC/MP3 play/pause/seek, and record timing and errors; then prepare the WSS protocol contract. No acoustic latency claim is currently supported.
