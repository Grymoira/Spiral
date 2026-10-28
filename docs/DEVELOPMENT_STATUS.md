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
the individual command schema to be present and non-`null`. Previously,
missing or `null` JSON scalars decoded to Go zero values (notably
`ready:false` and `mediaMs:0`), potentially mutating authoritative room
state. Exact field names, duplicate detection, type checking, and
command-specific allowed fields continue to use the existing parser;
valid `ready:false`, `mediaMs:0`, and `c0:0` retain their original meaning.

Regression tests enumerate missing/null for every required field, malformed
types, unexpected/duplicated fields and valid zero values. A real two-way
WebSocket test verifies rejected packets return `invalid_message` without
changing state revision, readiness or pending playback.

## Host control idempotency and optimistic revisions — 2026-10-27

Issue #19: duplicate share of identical SHA-256/duration becomes a no-op;
forced reload requires an explicit `reload:true`. Host `track` and
`schedule` WebSocket commands require an operation ID and authoritative
expected revision. Server-side atomic bounded deduplication (64 accepted IDs
per room) prevents retried commands from allocating new events or resetting
readiness. Reused IDs with different payloads and stale new intents return
conflict errors. The Qt client serializes controls pending command_ack and
retries in-flight IDs on reconnect. This changes the host control wire
contract; incompatible older clients must upgrade. No audio relay or acoustic
accuracy work is implied.

## Pending playback timer recovery — 2026-10-27

Issue #28: the Qt client now distinguishes one-shot pending events that are
armed, deferred due to unavailable media/clock/readiness, and already handled.
When an appointment cannot execute, the timer is disarmed and the deferred
event remains eligible only while the authoritative room still advertises the
same track-generation/event ID. On recovery, the client checks the current
recording and the member's authoritative Ready state before re-arming. No
previously executed event may play twice, and canceled/replaced events cannot
be resurrected by an old timer callback.

A playing-state appointment more than 250 ms late is conservatively skipped;
the local player pauses until a committed snapshot can reconcile playback.
A pending pause is still applied late when media becomes usable. This is a
safety threshold for avoiding late playback, **not** a claim of 250 ms acoustic
synchronization accuracy.

CI exercises the actual shipped QML functions under deterministic fake timer,
media, clock, Ready and snapshot conditions. Physical Qt multimedia/device
testing and true acoustic end-to-end timing remain outstanding (see #11).

## Live room expiry notifications — 2026-10-27

Issue #29: expiration is no longer exclusively passive when WebSocket clients
are connected. The room Store emits a nonblocking, coalesced expiry signal
under its lock for each expiration-driven member removal or room closure;
the signal carries no tokens, member names or room IDs. The Hub maintains
exactly one 500 ms expiry sweep loop while any socket is admitted. On a
signal it fetches fresh authoritative snapshots *after* releasing Store
locks, broadcasts the updated member list/revision and closes revoked
connections. Host expiry closes all sockets associated with the room.

The expiry loop is stopped when the Hub has no admitted sockets, so inactive
handlers do not accumulate permanent housekeeping goroutines. Session TTL,
five-member capacity, token authorization, playback safety and transport
policies remain unchanged. Notification is best effort within the local
process (not an external durable event bus); actual network delivery timing
depends on scheduling and connectivity.

Tests use an injected, concurrency-safe clock for HTTP/WebSocket expiry,
cover stale guest and host credentials, proactive sweeps, passive REST
cleanup, and absence of duplicate revision bumps. Existing Go race and Qt
CI remain release gates.

## Authenticated WebSocket idle heartbeat correction — 2026-10-27

Issue #30: previously, the 75-second authenticated read deadline was reset
only on application text messages; `socket.readText` consumed RFC 6455
Ping/Pong control frames inside its own loop without returning, causing valid
control-only sessions to time out despite heartbeat responses. The
authenticated read loop now provides an activity callback: **only valid,
completely parsed** Ping/Pong frames reset that idle deadline. The initial
8-second authentication deadline is unchanged; pre-auth control traffic
cannot extend it.

Because control frames bypass the Hub's text-message rate budget, the
WebSocket parser now also enforces a separate per-socket maximum of 80
Ping/Pong frames per rolling 10-second window, across text message
boundaries, including before authentication. The existing 4KiB text bound,
125-byte control-frame bound, masking/FIN rules and 25-second outgoing Ping
schedule are preserved. Tests exercise deadline callbacks, authentication
boundary, invalid frames, outbound Pong and control floods without a
75-second wall-clock wait. This is a transport keepalive fix, not a
guarantee of network connectivity or end-to-end audio synchronization.

## Issue #11 — software arming barrier (partial milestone; 2026-10-27)

This is a safety/protocol milestone, **not closure of Issue #11**. WebSocket
`play` and `seek` while the room is already playing now advertise a pending
`phase:"arming"` event with **no executable deadline**. Each authenticated
current Ready member must acknowledge the precise track generation and event
ID after the local recording, loaded media, and clock calibration are valid.
The server records ACKs under Store.mu and publishes `phase:"committed"` with
a new, shared deadline at least 2,500 ms ahead only once all members ACK.
An arming event with a missing ACK is canceled after 5 seconds.

A canceled/replaced event, stale generation, revoked Ready or replaced
connection cannot independently authorize playback. Client QML refuses to
execute any pending event whose phase is not `committed`. Paused-state
commands retain the earlier direct scheduling behavior. The internal
Store.Schedule helper remains backward compatible for in-process tests;
public WebSocket schedule commands use the new ACK barrier.

An ARM ACK confirms only application-level preparedness to receive COMMIT,
NOT DAC start, audible output or actual timeline alignment. Network partitions
after COMMIT, delayed server cancellation and audio driver buffering can still
produce divergent playback. End-to-end Qt multimedia output testing,
cancellation acknowledgement, physical two-device acoustical P50/P95/max
measurement with agreed tolerances, platform suspend/reconnect and partition
testing are still REQUIRED for Issue #11. No Release-level accuracy claim.

## Issue #11 phase 2 — authoritative readiness loss and partition watchdog

When a member loses Ready, disconnects, leaves/expires, or a new member joins
an already-playing room, the Store now immediately freezes the authoritative
media position at its monotonic server clock and pauses the room. Surviving
sockets receive an updated snapshot; disconnected devices cannot be assumed
to receive it. No existing authentication, TLS or room limits were relaxed.

The Qt client now requires recent proof of server connectivity during playing
and pending playback. A correctly validated room snapshot or clock-probe reply
refreshes a monotonic last-contact value (stored as QML real). A 700ms probe
cadence is enabled only when playback/pending is active, while the normal
15-second idle cadence is unchanged. After 2,500ms with no authoritative
response, the 500ms watchdog pauses audio and timers, revokes Ready on a
best-effort basis and requires fresh clock calibration before resumption.
Open socket status by itself is not proof of live connectivity.

This is a fail-closed *tradeoff*, not a global cancellation guarantee:
packet-loss spikes may pause healthy peers and process suspension or Qt GUI
event delays can extend the detection bound. The server cannot prove every
partitioned device received an abort; physical multimedia buffering and
two-device acoustic timing remain untested. Issue #11 remains OPEN.

## Issue #11 phase 3: member-loss appointment fencing — 2026-10-27

Starting from `main` `c15b6c35033e6ff4606cc8404e175f0f4b9706f4`,
this incremental correction addresses the precise Store regressions in #41 and
#42. On guest leave/expiry/join or Ready loss, pending playing appointments
(including already-ACKed COMMIT appointments) are invalidated. If playback
has already begun, suspension freezes the authoritative media anchor and
drops an obsolete future pause projection, without deleting paused-state seek.

Added Go Store regression tests for ARM/ACK/COMMIT then guest leave, expiry
and join, plus emergency suspension before a scheduled pause and preservation
of paused-state seek. The fix is fail-closed at the server; it does not
guarantee canceled snapshots arrive at partitioned clients.

**Verification status:** this GitHub-connector edit was not independently built
locally. The pull request's GitHub Actions result must be inspected for actual
Go race/vet/CTest and Qt build/QML test outcomes before merging. Explicit
CANCEL receipt semantics, physical two-device audio measurements and real
public-network partitions remain pending under #11.

## Issue #11 phase 4 — bounded application-level cancellation receipts (2026-10-27)

Baseline: `main` commit `9abef1ad491e8c53cf86db8aa891d55480fa1947`.
The Store now exposes a latest-event cancellation tombstone in room snapshots
and retains, at most, one outstanding-receipt map per room (no tokens or paths).
The realtime server validates `cancel_ack` against the authorized session,
room epoch, event generation, ID and cancellation revision; duplicates are
idempotent and return `cancel_ack_ok`. Qt cancels the matching local timer,
applies the authoritative paused state, then retries a narrowly scoped ACK
until acknowledged or the session changes.

Regression additions: Go Store tests cover incorrect event/generation/epoch/
revision/unauthorized ACKs, duplicate receipts, superseding tombstones and
departed members; a real HTTP + WebSocket integration test covers canceled
committed playback and receipt echoes; a production-function QML regression
tests local cancellation before ACK, retry, stale notices and socket shutdown.

**Evidence boundary:** this connector-side source edit is not a local Qt
runtime or physical audio verification. Inspect the PR and merged-main GitHub
Actions results before claiming the suites have passed. An ACK records an
application claim, not acoustic output. The latest-only receipt is not
durable history, is not an all-member acknowledgement safety barrier, and
cannot guarantee cancellation delivery during a partition. Real devices,
network faults, suspend/resume behavior and acoustic P50/P95/max remain
unfinished: Issue #11 must remain OPEN.

## Issue #11 phase 5: outstanding receipt gate — 2026-10-27

Source baseline: `main` commit `3e3ae9cad63840d7a1f531949b330614fa163746`.
In earlier phase 4, the server observed `cancel_ack` but had no safety
policy for absent ACKs. This increment makes the room reject an audible
playing transition while any surviving member has not acknowledged the
latest canceled event, and fences direct pending-to-play replacement. A
new safe control may supersede a prior cancellation, but the new latest
revision is treated as a cumulative watermark, resetting each member's
receipt obligation. Qt reconciles and cancels **all stale timers** before
ACKing this watermark, not only the exact event named by the notice.

Go Store regression tests cover missing and partial receipts, Ready restoration,
multiple rapid safe replacements, expired/removed member fences, a last-deadline
invariant and zero-mutation rejected controls. Real HTTP/WebSocket integration
deliberately withholds a guest's ACK while both sockets remain authenticated,
ensuring the host cannot publish a new ARM despite all Ready flags being true.
Qt production-function regressions cover skipped cancellation snapshots,
current-event preservation, retries and reconnect.

**Verification limits:** actual PASS results must be taken from this PR's
GitHub Actions and merged-main run. The test that omits an ACK is controlled
protocol fault injection, not a real OS sleep, Wi-Fi partition or acoustic
measurement. No guaranteed cancellation delivery or simultaneous DAC output
is claimed. A nonresponding current member can intentionally block room play;
there is no server eviction/recovery policy yet. Issue #11 must remain OPEN.

## Issue #11 phase 6 — OS clock discontinuity + correlated resynchronization (2026-10-27)

Baseline: `main` `33c244988676f61c0c1a07891f25e11d0a186017`.
QElapsedTimer is monotonic but may not count sleep time on every supported
platform. A skipped UI callback after suspend could therefore see a
pre-suspend authority lease as fresh. A new Qt-native continuity detector
compares elapsed monotonic and wall time; large divergence fails closed,
withdraws Ready, stops media and one-shot timer, drops unanswered probes,
and requests a new snapshot with an unpredictable, strictly validated ID.
Only the matching *authenticated* unicast snapshot releases the client's
recovery barrier, while the clock is recalibrated. Requests are retried
with a 1.2 s minimum interval. The server's `snapshot` request, previously
listed in the strict schema but unimplemented, now reads current Store
state and returns a correlated snapshot without mutating room revision.

Regression coverage: native Qt C++ clock-gap guard tests, shipped-QML
function tests that inject sleep before timer fire and stale clock replies,
and real Go WebSocket integration verifying authorized snapshot recovery,
operation ID echo, updated revision and malformed-message rejection.
CI runs these tests alongside Go race/vet/CTest and Qt compile/startup.

**Verification boundary:** test results are defined by the PR and merged-main
Actions runs and must not be reported successful until confirmed. Synthetic
time divergence is *not* a literal Windows/macOS/Linux suspend test. The
mechanism cannot prove acoustic silence, physical synchronization or
guaranteed delivery under partitions. Two-device acoustic P50/P95/max,
real suspend/reconnect, and fault tolerance acceptance remain required:
Issue #11 stays OPEN.

## Issue #11 phase 7 — fail-closed Qt buffering and measurable acoustic acceptance (2026-10-27)

Starting baseline: main `718b8f82de9c6e4a517d37d6ed0ee52c23119a6a`.
Static review exposed a remaining client failure path: Qt Multimedia may report
StalledMedia/LoadingMedia while playbackState remains PlayingState, but the
existing `onMediaStatusChanged` only withdrew readiness and `reconcile()`
could return early without locally pausing. A fast buffer recovery could
therefore resume playback from stale Ready before server cancellation arrived.
The new handler first pauses the pipeline and cancels local scheduled/ARM
bookkeeping, sends Ready(false) even if the previous local Ready key is gone,
and requires a fresh correlated authoritative (nonplaying) room snapshot
before Ready can be reannounced. MediaPlayer errors now fault the local source
even if Qt still reports BufferedMedia; the user must reselect a local file to
retry it. No media URLs, access tokens or local paths enter the room protocol.

The testing milestone now includes `tools/audio_alignment.py`, an **offline
stdlib-only** click generator and strict two-channel PCM16 onset-pairing
analyzer. It compares events sampled on one common stereo recording clock and
reports signed bias plus absolute P50/P95/max. Missing or unpaired clicks,
clipped samples, unknown predeclared limits, and too-few real event samples
cannot produce a success verdict. Hardware provenance is explicitly a
user-reported claim, not tool-authenticated evidence. Deterministic synthetic
WAV tests check offsets, missing events, clipping and invalid settings; the
physical two-device procedure lives in `tests/ACOUSTIC_ACCEPTANCE.md`.

**Do not equate CI with hardware success:** report the exact PR and merged-main
Actions results after their actual completion. Synthetic WAV offsets are
algorithm regression tests, not sound emitted by Qt. Real Windows/Linux/macOS
sleep, buffering, disconnect and combined acoustic acceptance—including
pre-approved physical thresholds and per-scenario audio captures—remain
unverified. Keep Issue #11 OPEN.

## Issue #11 phase 8 — silent-partition recovery requires fresh paused authority (2026-10-27)

Source baseline: main `cd40061a0c023c10f2241f165bb484b0a1583b38`.
A source-derived QML regression previously expected playback to resume from
its old `playing` snapshot after recalibration and a clock reply, **without**
a new authoritative room snapshot. That is unsafe when an apparently Open
WebSocket missed a cancellation or COMMIT. The authority watchdog now
withdraws Ready even when its cached key is absent, locally pauses/stops
timers, invalidates the old probe history, and enters the same correlated
read-only snapshot gate used after sleep and media failure. Clock responses
cannot release the gate; a matching nonplaying snapshot is required. Tests
cover the prior fail-open sequence, a mismatched reply, a matching still-playing
reply, clean paused recovery and lost local readiness bookkeeping.

The client-side recovery guard remains an application-level measure. It
cannot prove that actual output devices have become silent, cannot overcome
a coordinator that never responds, and is not proof of synchronized sound.
CI outcomes must be checked against the exact PR head and merged main,
not inferred. Physical two-device audio, literal suspend/network impairment,
and predeclared P50/P95/max acceptance remain unverified. Keep #11 OPEN.

## Issue #11 phase 9 — explicit host recovery from stuck cancellation ACK (2026-10-27)

Following phase 8 baseline main `3988d0a996ed0031e21d80991ef97bfe012a6aaf`,
introduce an explicit host-only manual guest removal action in the Go Store,
authenticated REST endpoint and Qt member list UI. A missing cancellation
receipt continues to block play **until** all living members acknowledge or
the host deliberately revokes the nonresponsive guest. Membership removal
suspends current play, revokes queued COMMIT, and removes only the departed
member's outstanding cancellation requirement. The host must start a **new**
ARM/ACK/COMMIT; revocation never counts a forged receipt.

Automated Store/API/QML/logging regressions cover authorization, self-target
rejection, revoked credentials, stale COMMIT fencing, loss of the missing-ACK
barrier after deliberate removal, token privacy and canonical route logging.
CI validation is pending until exact PR head and merged main workflows pass.

**Evidence boundary:** host removal cannot guarantee the physically isolated
guest has stopped its loudspeaker. Actual fail-closed disconnection capture,
OS/network failure injection, thresholds and dual-device P50/P95/max remain
mandatory before Issue #11 can be closed.

## v0.1.0-preview.1 Windows x64 packaging and distribution gate (2026-10-27)

A dedicated preview workflow builds the Qt 6.8.3 desktop client with MSVC
2022 x64 and the Go loopback coordinator on a Windows runner. It collects
runtime libraries, Qt QML modules and plugins with `windeployqt`, avoids
bundling private media/credentials, includes Spiral/third-party license
notices, tests packaged server `/healthz`, checks deployed desktop process
liveness without Qt SDK on PATH, and publishes a checksummed ZIP. This is
a proposed **GitHub Pre-release only**, created from a verified `main`
commit once its release pipeline succeeds. The Windows startup check is a
liveness probe, not an interactive GUI assertion; clean Windows, audio
device/codec output and acoustic synchronization are not tested in CI.

The accompanying `docs/PREVIEW_RELEASE_v0.1.0-preview.1.md` documents a
portable ZIP for Windows x64, the official MSVC redistributable requirement,
a localhost-only server, TLS-only cross-network testing and evidence
collection. macOS/Linux installers, signed packages, real-device verification
and full license audits across all deployed codecs are not claimed; leave
Issue #12 open. Issue #11 is also open until independently captured acoustic
P50/P95/max, network fault-injection and real OS suspend tests are accepted.
Always report actual release/PR CI results instead of assuming success.

## 2026-10-28 — P2P NAT traversal research checkpoint (documentation only)

Added [P2P connectivity research](research/P2P_CONNECTIVITY_RESEARCH.md),
cross-referenced by [Issue #70](https://github.com/Grymoira/Spiral/issues/70).
This compares public IPv4/IPv6 direct paths, explicitly mapped ports,
ICE + STUN with manual/other out-of-band signaling, and optional relay
fallback. It distinguishes no dedicated coordinator, no relay, and strictly
no third-party service. Sources include RFCs, peer-reviewed/published DCUtR
measurement evidence with its **conditional** hole-punching cohort,
libdatachannel C++ copy/paste and Pion Go examples, and a caveated NAT
simulation reference. The research documents a staged acceptance matrix,
failure boundaries, security requirements, and a proposed decision gate.

**Completed**: recorded research and an executable *plan* for a standalone
connectivity PoC. **Not performed**: third-party library build, ICE PoC,
NAT simulations, separate-network Windows verification, cross-country
verification, penetration testing, or real dual-device acoustic recording.
No production source code, current approved transport, privacy defaults,
build dependencies, or sync protocol changed. P2P is not implemented;
keep #70 OPEN. Real acoustic acceptance remains in #11 and is still
unverified. A new architecture ADR must be explicitly reviewed before
integration; this research checkpoint does not supersede ADR-0002.
