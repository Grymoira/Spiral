# Spiral experimental synchronized-room protocol

**Status: development prototype, not a frozen Sync Protocol v1.** Control data travels over a direct client-to-server WebSocket. The server is authoritative for the shared playback timeline; audio files never pass through the room server. Devices on different networks connect to the same reachable HTTPS/WSS endpoint. Spiral does not currently establish peer-to-peer NAT-traversing sockets between devices.

## Transport and access

- Same listener serves HTTP(S) room membership and `GET /v1/ws` (WebSocket upgrade).
- Plain HTTP/WS are limited to loopback, including the actual bound address. All non-loopback binds require `-tls-cert` **and** `-tls-key`; use a trusted TLS certificate and `https://` / `wss://` in clients. TLS certificate verification is never disabled by default.
- New WebSockets must send `{ "type":"auth", "roomId":"...", "sessionToken":"..." }` as their **first text frame**. Session tokens are never included in URLs, room snapshots, media metadata, or logs. Session expiry and revocation are checked for each command.
- Native clients need not set `Origin`; present browser origins must match the target origin. WebSocket messages are bounded to 4096 bytes, must be masked UTF-8 text, and do not use extension/compression or fragmented frames. Invalid/oversized messages terminate the connection. A basic per-connection rate limit applies.
- A member's newer authenticated WebSocket replaces the prior one. Disconnect clears that member's readiness; reconnect requires fresh time sampling and readiness confirmation. There is no durable resume across server restarts.

## HTTP room membership

Requests must be JSON objects with *exactly cased* allowed property names, at most 4096 bytes, unique fields, and no trailing JSON. Replies use `Cache-Control: no-store`.

| Method | Path | JSON/credentials | Result |
|---|---|---|---|
| GET | `/healthz` | none | `{ "status": "ok" }` |
| POST | `/v1/rooms` | `{}` | Host credentials and invitation |
| POST | `/v1/rooms/{id}/join` | `{ "inviteCode":"..." }` | Member credentials |
| GET | `/v1/rooms/{id}` | `Authorization: Bearer <token>` | Current authoritative snapshot |
| DELETE | `/v1/rooms/{id}/members/me` | Bearer token | Exit; host exit destroys room |

A room has at most five active members. Session tokens expire after 24 hours and are stored only in server memory. State is ephemeral and erased on restart.

## Real-time messages (JSON)

Client to server:

```json
{"type":"auth","roomId":"ROOM_ID","sessionToken":"SECRET"}
{"type":"clock_probe","probeId":"1","c0":12345}
{"type":"track","sha256":"64 lowercase hex digits","durationMs":60000}
{"type":"ready","trackId":"64 lowercase hex digits","trackGeneration":1,"ready":true}
{"type":"schedule","action":"play","mediaMs":0}
{"type":"schedule","action":"pause","mediaMs":0}
{"type":"schedule","action":"seek","mediaMs":15000}
```

Host `track` and `schedule` require `operationId` and `expectedRevision` (see below); they also require host permissions. `ready` requires the exact current track hash and generation. `play` (including playing-state seek) is rejected until *every active member* reports ready. Readiness is advisory client state, not cryptographic proof of actual audio output. All file paths and media credentials stay on clients.

Server to client:

- `auth_ok` — authenticated member ID, role, clock epoch.
- `snapshot` — complete room membership and timeline state, including `roomEpoch`, `clockEpoch`, `stateRevision`, server monotonic time, SHA-256 track identity, `trackGeneration`, audio duration, playing/paused state, media/time anchors, and optional pending event.
- `clock_reply` — echoed `probeId`, `c0`, and server receive/send monotonic timestamps `s1`, `s2`, with `clockEpoch`.
- `command_ack` — acknowledgement of accepted host controls (`operationId`, `stateRevision`, `applied`).
- `error` — machine-readable error code (`unauthorized`, `forbidden`, `not_ready`, `no_track`, `invalid_state`, `invalid_message`, `rate_limited`, etc.).

Sample clock estimate: `offset_server_minus_client = ((s1-c0)+(s2-c3))/2`, `RTT=(c3-c0)-(s2-s1)`. A client measures `c0`/`c3` using its own monotonic timer. Estimate quality degrades when RTT is too high or the sample gets stale. Monotonic time in milliseconds is meaningful only inside the named clock epoch; it must not be compared to UTC timestamps or retained across server restarts.

### Scheduled action semantics

When authorized, the server accepts a command and broadcasts one `pending` event with a unique ID and a target `executeServerMs` at least **2500 ms** ahead of server time. All current ready clients translate that target using their estimated server clock and schedule local player operations. At deadline the server atomically commits the event and broadcasts a new snapshot. New host commands replace older pending events, and stale timers are rejected by ID and track generation. Clients compare `stateRevision` and `trackGeneration`; control messages do not carry media URLs or paths. Pause/seek use the server's authoritative anchor. During disconnection, the prototype pauses local playback rather than free-running.

This is a *best-effort* synchronized playback baseline. There is no distributed arm/ack/commit consensus, guaranteed simultaneous audio output, jitter buffer calibration, or verified audio-device latency model. A `QMediaPlayer.position` match is not proof of acoustic synchronization. Cross-network public deployments still require abuse-defense hardening, managed TLS termination, and real devices for testing.

### Anonymous room creation guard (prototype)

Public `POST /v1/rooms` requests have per-source concurrent-room and
rolling-window creation quotas, plus a global creation-rate budget.
Requests over the rolling rate budget receive HTTP 429
`room_creation_rate_limited` and `Retry-After: 60`; sources already at
their concurrent-room allowance receive HTTP 429
`source_room_limit_reached`. New rooms not claimed via an authorized
snapshot, authenticated WebSocket, or valid join expire after 15 minutes,
even though their issued session credentials otherwise have a 24-hour
lifetime. These admission limits do not apply to existing room join or
playback operations. The source address is the direct TCP peer, not an
untrusted forwarding header; a TLS-terminating reverse proxy must be
deployed with deliberate source attribution and edge controls.

### Required WebSocket message fields

Each client command requires **all fields shown in its JSON example** above.
The exact names and JSON types are mandatory: `auth` requires `type`,
`roomId`, `sessionToken`; `clock_probe` requires `type`, `probeId`, `c0`;
`track` requires `type`, `sha256`, `durationMs`; `ready` requires
`type`, `trackId`, `trackGeneration`, `ready`; and `schedule` requires
`type`, `action`, `mediaMs`. Missing, `null`, wrongly typed, duplicated,
or command-inappropriate fields are rejected with `invalid_message`.
Intentional scalar zero values retain their meaning: `ready:false`,
`mediaMs:0`, and `c0:0` are accepted if the rest of the command is valid.
Domain-specific validation (such as track identity, duration and permissions)
still follows parsing and returns its existing operation error codes.

**Compatibility note:** earlier client examples above illustrate payload contents; add the new required `operationId` and `expectedRevision` fields for `track` and `schedule` before sending. Legacy host controls are rejected as `invalid_message`.

### Authenticated WebSocket heartbeat

The server emits RFC 6455 Ping frames every 25 seconds. Following successful
authentication, a valid masked client Ping or Pong counts as peer activity and
refreshes the server's 75-second read-idle deadline even when no application
JSON message is sent. Client-initiated Ping frames receive matching Pong
replies. This does not extend the initial 8-second authentication deadline:
pre-auth control frames cannot delay authentication timeout.

To bound control-frame floods that bypass the ordinary text-message rate
limit, each socket accepts at most 80 Ping/Pong frames per 10-second window;
the budget persists across JSON text messages and also applies pre-auth.
Malformed, non-final, oversized or unmasked frames are rejected under the
existing strict WebSocket rules. Client protocols may use control frames for
keepalive, but valid credentials, session TTL and authoritative access checks
remain mandatory.

### Playing-state ARM / ACK / COMMIT (Issue #11, partial protocol)

All authenticated WebSocket `schedule` controls that would enter a
**playing** state (play, or seek while already playing) initially publish a
pending event with `phase:"arming"` and an `armExpiresServerMs` deadline
approximately 5 seconds in the future. These events have
`executeServerMs:0` and are **not executable**.

Each currently authorized, Ready member with the same locally loaded
recording and a calibrated clock must send:
`{"type":"arm_ack","trackId":"<sha256>","trackGeneration":1,"eventId":7}`.
Only the event's exact current room membership, track generation, ID,
and Ready status are accepted. Each member is counted once; duplicate ACKs
do not advance the barrier. When all current members have ACKed, the server
releases `phase:"committed"` with a newly allocated shared execution
deadline at least 2.5 seconds in the future. A pending ARM without every
ACK is canceled after 5 seconds, without starting playback. Obsolete
revisions, readiness revocation and new members can cancel an ARM.

Pause and seek while paused continue using direct `phase:"committed"`
scheduling; they do not initiate audible playback. Clients MUST refuse
local playback from an `arming` event and MUST upgrade together with the
server for this protocol version.

ACK means **application prepared to schedule**, not physical audio-start
confirmation. Delivery failure after COMMIT, network partitions and audio
output latency are still outside proven correctness. Issue #11 remains open
pending physical two-device capture and P50/P95/max measurements.

### Playing-state fail-closed suspension (Issue #11, phase 2)

The authoritative room freezes its media anchor and enters `paused` when
an active member revokes Ready, loses its WebSocket, leaves/expires or a new
unready member joins an already-playing room. Existing surviving sessions
are informed by room snapshots. An unreachable device cannot be guaranteed
to receive an asynchronous cancellation.

The Qt desktop client uses a 2,500ms recent-authority requirement during
playback or scheduled play and a 700ms clock-probe cadence while playing or
pending. A validated clock probe reply or an accepted room snapshot refreshes
the contact timestamp. If no such event arrives for 2.5 seconds, the client
pauses playback and timers, attempts to withdraw Ready and requires fresh
clock calibration. Normal idle probing remains 15s. This is *bounded local
best-effort detection*, not consensus or physical acoustic synchronization.
OS suspension, packet loss and Qt scheduling delay can exceed this window.

### Pending-event fencing after membership/readiness loss (Issue #11, phase 3)

If a member withdraws Ready, leaves via REST, expires, or a new member joins,
the server cancels an already committed but not-yet-executed playing event
even when all remaining members are Ready. The same rule applies to uncommitted
arming events. A forced suspension of an already-playing timeline discards
a stale future-projected pause to avoid moving the frozen media anchor forward
at its former deadline. Explicit seeks scheduled while already paused are
preserved.

The authoritative snapshot reports the resulting state/revision, but delivery
during a network partition is not guaranteed. Explicit event-scoped CANCEL
receipt semantics, real Qt output and acoustic measurements remain required
before closing Issue #11.

### Event-scoped cancellation receipt (Issue #11, phase 4 prototype)

When an unexecuted pending appointment is retired by a new track/command,
readiness or membership loss, or an expired ARM timeout, the authoritative
snapshot includes an optional `cancellation` tombstone with `eventId`,
`trackGeneration`, and the `stateRevision` that retired it. The room's
existing `roomEpoch` identifies the room instance. Only the **latest**
canceled pending event is retained (at most five member receipts per room);
this is intentionally not a durable cancellation history.

After applying the authoritative state and stopping any local timer for that
exact event (and pausing local playback when the room is paused), the client
sends this authenticated WebSocket message:

```json
{"type":"cancel_ack","roomEpoch":"ROOM_EPOCH","trackGeneration":1,"eventId":7,"stateRevision":12}
```

The server checks the member's current authenticated socket, room epoch,
track generation, event ID and cancellation revision. It counts at most one
receipt per current member, returns `cancel_ack_ok` with the same identifiers,
and accepts identical retries as no-ops. The Qt client resends at most about
once per 1.2 seconds until this response arrives, and retries after reconnect.
A departing/expired member is removed from the outstanding receipt set.
No token or local media path enters the cancellation record or log.

**Safety limits:** This is a verifiable *application-level statement* that a
client processed a cancellation, not proof of physical acoustic silence.
Receipt absence does not itself hold the room in a blocked state; a newer
cancellation replaces the old record, so late ACKs for a superseded notice
are rejected. A partitioned/offline client may never receive the snapshot,
and the existing 2.5-second watchdog remains the local fallback. No explicit
delivery guarantee, durable receipt history, deterministic partition safety,
or real-device output timing has been established. Old clients lacking this
command will not produce receipts; coordinated upgrades are needed to use
receipt diagnostics meaningfully. Issue #11 stays open.

### Cancellation ACK playback barrier and cumulative watermark (Issue #11, phase 5)

**Fail-closed admission:** While the room's current cancellation notice has
unacknowledged members, the server rejects any new `play` or seek-while-playing
control with `cancellation_pending`. It also refuses to replace an existing
pending appointment directly with a playing-state command: such replacement
would itself cancel a previous appointment without a receipt. These checks
apply to both the WebSocket and the internal Store scheduling API. The final
COMMIT deadline rechecks the cancellation barrier, and an unexpected failure
retires the event rather than starting audio.

A notice remains the **latest cumulative cancellation watermark** for the
room: receiving its authoritative snapshot supersedes *all* local
appointments not present as the current `playback.pending`. A client must
discard every obsolete one-shot timer and ARM/deferred marker before it
acknowledges the latest notice. When another safe pause/paused-state seek
replaces a pending appointment before all prior receipts arrive, a newer
watermark is issued and each surviving member must acknowledge it afresh.
Older acknowledgements cannot clear a newer watermark. Existing live members
that leave or expire are removed from the required-ACK set. Safe paused-state
controls, readiness withdrawal and host room teardown are not blocked by the
receipt barrier.

**Availability and security tradeoff:** A member with a live session that
never sends an ACK blocks further coordinated play. This is intentional:
silently timing out the ACK into implicit approval would defeat fail-closed
behavior. No administrator kick, timeout eviction, durable cancellation
history, or receipt persistence across server restarts is implemented.
Clients running older protocol versions will fail to acknowledge and can
therefore block playback. The connection must be upgraded consistently across
all members. A forged application ACK from an already authenticated client is
not proof of acoustic silence. Physical output timing and network partition
after COMMIT remain outside the demonstrated guarantee.

### Recovering after OS suspend / clock discontinuity (Issue #11, phase 6)

A Qt client may observe that its monotonic clock advanced much less than
UTC wall time while the device was asleep. `SyncTools::clockContinuityOk()`
compares these elapsed intervals on each relevant callback. A difference
greater than 1500 ms, a reversed clock, or a reset monotonic clock triggers a
**local fail-closed pause**, withdrawal of Ready, cancellation of any scheduled
local timer, and reset of the clock-probe model. The client must obtain a
correlated fresh authority snapshot and recalibrate before it may become
Ready again. This guard complements the 2.5-second authority-contact lease
and 250 ms late-commit skip; it does not use UTC wall time for scheduling
audio, and a local clock adjustment may intentionally trigger a false alarm.

After discontinuity, the authenticated client sends:

```json
{"type":"snapshot","operationId":"unique-safe-identifier"}
```

The server validates an 8–64 character operation identifier (ASCII
letters, digits, underscore or hyphen), verifies the current authenticated
WebSocket peer and returns a **unicast**, read-only, correlated response:

```json
{"type":"snapshot","operationId":"unique-safe-identifier","state":{"roomId":"...","stateRevision":7}}
```

The state above is abbreviated; actual responses contain the existing
complete authoritative room snapshot. No access token, media URL, absolute
local file path or invitation is included. A broadcast `snapshot` without
the matching `operationId` **must not** release the local wake barrier.
The client retransmits its *same* recovery request, at most once per
1.2 seconds, until a matching response arrives. A reconnect resets the
old request and starts a new authenticated session snapshot sequence.

**Limits:** The 1500 ms clock-divergence test cannot detect every suspend
pattern; on platforms where the monotonic clock advances with wall time,
ordinary elapsed authority freshness and late-event checks remain decisive.
The mechanism prevents using old local timers after detected discontinuities
but does not guarantee timely cancellation delivery during a hard network
partition, and still does not measure DAC/acoustic output.

### Multimedia buffering and fault fencing (Issue #11, phase 7)

A local Qt Multimedia transition out of LoadedMedia/BufferedMedia is not only
a readiness withdrawal: while the backend still indicates PlayingState,
playback may remain audible. The client now **pauses immediately**, retires
its local one-shot timer, and sends Ready(false) for the current room track.
It then invokes the same correlated snapshot recovery fence already used
after sleep; restoring a locally buffered media state **cannot** autoplay a
stale committed event or mark the member Ready until a fresh server snapshot
confirms a nonplaying timeline.

Decoding/device errors are treated as a local permanent media fault even when
the backend's last status was BufferedMedia. A re-selected source explicitly
clears that fault; this is fail-closed and may sacrifice availability.
Clients must keep the room's source hash/generation private to protocol
control only—never send local file paths, media URLs or credentials.

Offline acoustic acceptance tooling and its strict evidentiary boundary are
in `tests/ACOUSTIC_ACCEPTANCE.md`. Application-level Ready/ACK and synthetic
PCM test signals **do not** prove true acoustic output synchronization.

### Silent-partition reauthorization fence (Issue #11, phase 8)

A stalled authenticated WebSocket can remain in Qt's `Open` state while no
new server state arrives. Before this change, the 2.5 s authority watchdog
paused playback and invalidated clock calibration, but fresh `clock_reply`
frames could restore calibration and reauthorize an **old playing snapshot**
without any new room-state confirmation. Clock freshness is not state freshness.

On watchdog expiry during an audible or queued playing transition, Qt now
pauses locally, stops queued timers, clears obsolete playing-event bookkeeping
and probe history, and explicitly sends `Ready(false)` even if its cached
Ready key is already empty. While the socket remains Open it starts the
existing correlated `snapshot` recovery protocol with one UUID and bounded
retries. A merely fresh clock probe, broadcast snapshot, mismatched reply,
or a matching snapshot still showing playing / pending play **cannot**
release this fence. The server must first respond to that request with a
nonplaying authoritative state; only then may recalibration and subsequent
Ready checks resume. Socket disconnection uses the existing fresh-auth
reconnect path, which revokes old server-side Ready.

This is a **fail-closed availability trade-off**: a still-playing room
requires an authoritative pause before a partitioned client can rejoin.
A malicious or entirely unreachable coordinator cannot be forced to answer,
and locally pausing a Qt player does not constitute measured DAC/acoustic
silence. The 2.5 s timeout bounds *detection under an active timer*,
not guaranteed physical stopping latency during OS suspend; the actual
two-device and OS fault-injection evidence required by #11 is still absent.
