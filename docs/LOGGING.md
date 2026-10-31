# Logging contract (schema v1)

Spiral emits **structured JSON Lines (JSONL)**. Each record is one UTF-8 JSON
object on exactly one line. This is diagnostic event logging, **not** a message
body trace, packet capture, authentication audit ledger or acoustic proof.

## Common schema

| Field | Type | Meaning |
| --- | --- | --- |
| `timestamp` | string | UTC ISO-8601 / RFC3339 with fractional seconds |
| `level` | string | `DEBUG`, `INFO`, `WARN`, or `ERROR` |
| `event` | string | Stable lowercase dot-separated event code |
| `component` | string | e.g. `http`, `websocket`, `server`, `desktop` |
| `service` | string | `spiral-server` or `spiral-client` |
| `schema_version` | number | Currently `1`; bump when field meanings change |

Server REST events also provide `request_id`, HTTP `method`, a fixed canonical
`route` name, numeric `status`, and `duration_ms`. WebSocket lifecycle and
command events have an ephemeral, per-process `connection_id`; scheduled
commit events include action and numeric event/generation identifiers. Qt
client events include an ephemeral `session_id` distinct from room identity.

Example, with illustrative IDs:

```json
{"timestamp":"2026-10-27T00:00:00Z","level":"INFO","event":"http.request_completed","service":"spiral-server","schema_version":1,"component":"http","request_id":"http-0000000000000001","method":"POST","route":"rooms.create","status":201,"duration_ms":0}
```

## Data minimization and protection

- The logger **never** prints request bodies, arbitrary URL paths or queries,
  authorization headers, cookies, invite codes, session tokens, media URLs,
  absolute audio paths, chat content, remote IP addresses or a raw WebSocket
  payload. Route names and client event names are fixed allowlists.
- Client events use fixed codes only; QML cannot submit an arbitrary string as
  a log event. User-facing error text is **not** automatically added to logs.
- Server logs go to standard output; control access, collection retention and
  redaction at the deployment layer. Set `-log-level warn` for reduced volume.
- Desktop logs are written to the Qt application-local data directory as
  `events.jsonl`, with one rotated `events.jsonl.1` file. Rotation occurs
  once a file reaches 2 MiB. Logs do not include recording paths or hashes.
- The `X-Request-ID` HTTP response header helps correlate client reports with
  a server REST log without exposing room identifiers.
- Failures to write diagnostic logs must not prevent playback or leak media
  credentials to a fallback logging channel.

## Gaps and follow-up work

Logging currently covers major connection lifecycle, authentication, validation,
room synchronization and player-state transitions. It is **not** a comprehensive
operational telemetry suite: durable audit storage, deployment-level anti-abuse,
metrics/tracing, secure incident response, and detailed acoustic measurements
are not implemented. Chat and danmaku (bullet-screen messages) remain deferred
until the core multi-device audio experience has been measured and hardened.

## Issue #11 host recovery event codes

An authenticated manual guest removal emits the fixed REST route label
`rooms.member_remove` (not a dynamic room/member identifier). A successful
desktop host action emits `client.member_removed`. Both use the existing
redacted event schema and never expose credentials or member IDs.

## Issue #11 phase 11 — bounded Qt playback diagnostics (Draft)

The experimental desktop build adds fixed, allowlisted client event codes to
separate the software control sequence from actual Qt player state. This does
**not** prove audio reached an output device or that two physical sound waves
were synchronized. The existing JSONL schema and 2 MiB rotation still apply.

- `client.play_request_timer` / `client.play_request_snapshot`: the timer or
  authoritative snapshot path is about to call `QMediaPlayer.play()`.
- `client.player_state_playing` / `client.player_state_not_playing`:
  `playbackStateChanged` signaled a Qt playback-state transition.
- `client.player_position_advanced`: the player-reported position advanced by
  at least 150 ms from a request's starting position; logged at most once per
  play request. Neither this nor `PlayingState` establishes acoustic output.
- `client.media_state_buffering`, `client.media_state_stalled`,
  `client.media_state_loaded`, `client.media_state_other`: media status
  classification when the existing status handler executes.
- `client.ready_withdraw_media`, `client.ready_withdraw_clock`,
  `client.ready_withdraw_clock_expired`, `client.ready_withdraw_authority`,
  `client.ready_withdraw_wake`, `client.ready_withdraw_explicit`: a fixed
  reason logged before each corresponding outgoing `ready:false` operation.
  The server can also clear readiness on socket loss; this is not an outgoing
  ready command from the client.

All codes are constant strings with no room ID, invitation, token, URL, file
path, media hash, free-form message or device name. To correlate instances,
filter by the existing ephemeral `session_id` and UTC timestamp. For Issue #11,
compare a successful `playback.committed` timestamp against `play_request_*`,
`player_state_*`, `player_position_advanced` and `ready_withdraw_*` events.
The diagnostic build is an investigation aid only; do not close #11 until
actual audible playback and controlled physical measurements pass.
