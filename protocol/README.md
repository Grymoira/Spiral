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

`track` and `schedule` require host permissions. `ready` requires the exact current track hash and generation. `play` (including playing-state seek) is rejected until *every active member* reports ready. Readiness is advisory client state, not cryptographic proof of actual audio output. All file paths and media credentials stay on clients.

Server to client:

- `auth_ok` — authenticated member ID, role, clock epoch.
- `snapshot` — complete room membership and timeline state, including `roomEpoch`, `clockEpoch`, `stateRevision`, server monotonic time, SHA-256 track identity, `trackGeneration`, audio duration, playing/paused state, media/time anchors, and optional pending event.
- `clock_reply` — echoed `probeId`, `c0`, and server receive/send monotonic timestamps `s1`, `s2`, with `clockEpoch`.
- `error` — machine-readable error code (`unauthorized`, `forbidden`, `not_ready`, `no_track`, `invalid_state`, `invalid_message`, `rate_limited`, etc.).

Sample clock estimate: `offset_server_minus_client = ((s1-c0)+(s2-c3))/2`, `RTT=(c3-c0)-(s2-s1)`. A client measures `c0`/`c3` using its own monotonic timer. Estimate quality degrades when RTT is too high or the sample gets stale. Monotonic time in milliseconds is meaningful only inside the named clock epoch; it must not be compared to UTC timestamps or retained across server restarts.

### Scheduled action semantics

When authorized, the server accepts a command and broadcasts one `pending` event with a unique ID and a target `executeServerMs` at least **2500 ms** ahead of server time. All current ready clients translate that target using their estimated server clock and schedule local player operations. At deadline the server atomically commits the event and broadcasts a new snapshot. New host commands replace older pending events, and stale timers are rejected by ID and track generation. Clients compare `stateRevision` and `trackGeneration`; control messages do not carry media URLs or paths. Pause/seek use the server's authoritative anchor. During disconnection, the prototype pauses local playback rather than free-running.

This is a *best-effort* synchronized playback baseline. There is no distributed arm/ack/commit consensus, guaranteed simultaneous audio output, jitter buffer calibration, or verified audio-device latency model. A `QMediaPlayer.position` match is not proof of acoustic synchronization. Cross-network public deployments still require abuse-defense hardening, managed TLS termination, and real devices for testing.
