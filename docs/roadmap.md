# Roadmap — Spiral

The original SyncMusic research described Phase 0–7, but no corresponding source files existed in this Spiral repository at initialization. These are the milestone mappings for work going forward; completing a source file is not equivalent to validating the milestone.

- [ ] **Phase 0 / Milestone A — reproducible baseline.** Go room core and HTTP development server, tests, initial Qt local audio player and CMake/CI scaffolding are implemented. Go baseline tested on Linux. Qt Linux build passed CI 40364849956; runtime, Windows/macOS builds and audio tests remain unverified. A follow-up review hardened loopback binding, JSON validation, method responses and CMake missing-toolchain behavior; local Go tests and the Linux server-only build pass.
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
- [ ] **Next required acceptance:** run the CI-built Qt client on a supported OS, fix any QML/runtime errors, and perform two physical-device audio capture tests with quantified P50/P95/max alignment error.
- [ ] Add authenticated WSS deployment soak tests, reconnection/clock regression tests, bounded state and per-IP abuse controls, and end-user packaging.

## 2026-10-27: correctness and observability increment

- [x] Revoke stale scheduled play on Ready loss and late joins; defend at commit.
- [x] Bound WebSocket sockets per IP and globally; reject superseded sessions.
- [x] Define a privacy-preserving JSONL logging schema for Go and Qt desktop.
- [x] Add regression coverage and request Qt headless startup verification in CI.
- [ ] Validate the QML runtime on CI (must check new workflow result), then on
      real Windows, Linux and macOS desktop devices.
- [ ] Add explicit ARM/ACK/COMMIT with cancellation/partition semantics before
      claiming reliable simultaneous acoustic output.
- [ ] Design chat/danmaku as a separate authenticated and rate-limited feature
      once multi-device audio reliability is demonstrated.
- [ ] Strengthen public deployment security (identity/rate limits, reverse proxy,
      abuse testing, secret rotation and operational incident controls).

## 2026-10-27: pending one-shot recovery hardening

- [x] Track locally armed/deferred/handled pending events separately and retry
      safe recoverable events after media, clock and readiness become usable.
- [x] Do not replay a completed, replaced or withdrawn pending event; fail
      conservatively on substantially late playing-state deadlines.
- [ ] Validate callback behavior with real Qt multimedia backends, buffering
      transitions and two physical machines; CI QML timer fakes alone do not
      demonstrate synchronized acoustic playback (see #11).

## 2026-10-27: live session expiry propagation

- [x] Coalesce expiration notifications from authoritative Store cleanup and
      broadcast current room revision to connected members.
- [x] Run one bounded housekeeping loop while the Hub has admitted sockets;
      shut it down when no sockets remain.
- [x] Close room sockets when the host expires; reject revoked guest tokens.
- [ ] Longer-lived sessions require explicit authenticated renewal and
      cross-process expiry event propagation if using more than one server
      instance; the current in-memory prototype remains single-process.

## 2026-10-27: WebSocket heartbeat idle-timeout correction

- [x] Refresh the authenticated 75-second idle deadline on valid Ping/Pong
      rather than only on application text frames.
- [x] Keep the pre-auth 8-second timeout unchanged and rate-limit control
      frames independently, across application-text message boundaries.
- [ ] Validate long-lived public WSS sessions and intermittent mobile
      networks under actual network partition, packet loss and proxy behavior.

## Issue #11: incremental ARM/ACK/COMMIT safety gate

- [x] Require event-scoped per-member ACKs for authenticated WebSocket
      playing-state schedules before any executable deadline is published.
- [x] Fail closed when an ARM expires unacknowledged, reject stale IDs and
      fence phase/deadline timer races.
- [x] Send Qt arming ACK only with local track, media, Ready and clock checks,
      and refuse to execute an uncommitted pending event.
- [ ] Verify real Qt Multimedia state transitions, explicit CANCEL ACK,
      network partitions after COMMIT, OS suspend/reconnect and two-device
      recorded acoustic onset differences. Issue #11 remains OPEN.

## Issue #11 phase 2: fail-closed active playback

- [x] Freeze authoritative playhead on Ready loss, disconnect, member
      join/leave and expired guest, preserving monotonic media position.
- [x] Require recent verified coordinator response during queued/audible
      playback, with a 2.5s watchdog and 700ms targeted clock probes.
- [ ] Implement verifiable cancellation receipt/partition semantics and
      perform real Qt playback, suspend/reconnect, fault injection and
      two-device acoustic P50/P95/max acceptance. Issue #11 remains OPEN.

## Issue #11 phase 3: membership-loss pending-event fencing

- [x] Revoke already committed playing appointments on guest leave, expiry and
      join, regardless of the remaining members' Ready flags.
- [x] Discard the obsolete future pause projection when emergency suspension
      freezes active playback, preserving legitimate paused seeks.
- [ ] Design event-scoped cancellation receipts, network fault injection and
      cross-platform acoustic tests. Issue #11 stays open.

## Issue #11 phase 4: bounded cancellation receipts

- [x] Attach an event ID, track generation and cancellation revision to the
      latest server-retired pending appointment in authoritative snapshots.
- [x] Require authenticated, current-member `cancel_ack` messages; retain
      bounded per-member acknowledgement state and reply idempotently.
- [x] Have Qt apply a matching cancellation before acknowledging; throttle
      retries and retry following reconnect. Add Store, WebSocket and QML tests.
- [ ] Define policy when not all acknowledgements arrive (block, evict or
      otherwise fail closed) and how to handle multiple rapid cancellations,
      hard network partitions, sleep/wake and actual acoustic silence.
- [ ] Measure two real devices' onset error (P50/P95/max) with an explicit
      test methodology and acceptance thresholds. Keep Issue #11 OPEN.

## Issue #11 phase 5: enforce cancel-before-next-play

- [x] Reject a new playing-state control until every surviving member ACKs the
      latest cancellation revision; reject direct pending-to-playing replacement.
- [x] Keep receipts cumulative across rapid safe cancellations; invalidate
      obsolete QML timers before the latest watermark ACK.
- [x] Recheck cancellation fencing at COMMIT; broadcast a resulting canceled
      revision even when a deadline does not commit.
- [x] Add Go Store and real two-WebSocket withheld-ACK fault-injection tests,
      plus shipped-QML rapid-replacement and reconnect regression coverage.
- [ ] Test true network interruption/OS suspend with Qt Multimedia devices;
      no software ACK establishes acoustic silence.
- [ ] Define and collect repeatable two-device onset/pause/seek drift
      measurements, reported as P50/P95/max with explicit acceptance limits.
- [ ] Decide practical fail-closed recovery for unresponsive-but-connected
      members (host removal, timeout expulsion, or admin intervention) without
      treating a missing ACK as approval. Keep #11 OPEN pending these items.

## Issue #11 phase 6: suspend/wake and explicit authority recovery

- [x] Add native Qt monotonic-vs-wall elapsed-time continuity guard and
      simulate sleep, clock rollback and calibrated recovery in C++ unit tests.
- [x] Pause immediately and revoke Ready on detected discontinuity before
      handling old playback timers or delayed time-probe replies.
- [x] Add strict, authenticated `snapshot` request/response with a fresh
      operation identifier, read-only unicast semantics and bounded retries.
- [x] Require a matching recovery snapshot before automatic Ready or playback
      may resume; add Go real-WebSocket and production-QML fault-injection tests.
- [ ] Confirm behavior on Windows, Linux and macOS with real OS suspend/resume,
      media buffering, abrupt partitions and audio hardware.
- [ ] Collect synchronized dual-device microphone/loopback measurements for
      play, pause, seek and track changes, with P50/P95/max and acceptance
      thresholds established prior to testing. Keep Issue #11 OPEN.

## Issue #11 phase 7 — buffering fail-closed & offline acoustic acceptance

- [x] Pause Qt Multimedia immediately when a previously usable recording
      transitions into unavailable/stalled status, regardless of PlayingState.
- [x] Treat Qt decoding/output errors as local media faults even if the media
      status still claims BufferedMedia; block retry until file re-selection.
- [x] Withdraw authoritative Ready(false), fence queued COMMIT events, and
      require correlated fresh paused-state confirmation before Ready resumes.
- [x] Run actual shipped QML handler regression tests for lost/restored buffer,
      still-playing remote state, pending timer retirement and faulted media.
- [x] Add offline, Python-stdlib mono click-track generation and strict common
      stereo-clock WAV alignment analysis with raw P50/P95/max and incomplete
      recording rejection. Add deterministic **synthetic-only** analyzer tests.
- [x] Document physical capture layout, threshold declaration, evidence
      provenance, and required full play/pause/seek/network fault matrix.
- [ ] Run physical Qt multimedia fault/restore cases, two independent network
      paths, OS suspend, real stereo acoustic capture and pre-registered
      multi-platform P50/P95/max verdicts. Issue #11 remains OPEN.

## Issue #11 phase 8 — explicit authority after a silent partition

- [x] Prevent clock-only recovery from reusing a pre-partition playing
      snapshot on a Qt WebSocket that still appears Open.
- [x] Force Ready(false) even when the local Ready key was lost, clear
      old timers/probes, and require the correlated nonplaying snapshot gate.
- [x] Add source-derived QML regressions for the stale-snapshot replay
      and positive/negative recovery responses.
- [ ] Verify the actual network-loss/restore path and paused audible output
      on physical clients with separate network links.
- [ ] Obtain actual two-device capture evidence and approved onset, pause,
      seek and track-change acceptance criteria. Keep #11 OPEN.
