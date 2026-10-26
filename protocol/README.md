# Prototype protocol — NOT Sync Protocol v1

This document describes only the current loopback HTTP membership experiment. It does **not** define the future WSS synchronization wire protocol.

All endpoints are local-development-only. All requests carrying JSON must use `Content-Type: application/json` and be no larger than 4096 bytes. Unknown JSON properties and extra JSON values are rejected. Replies are JSON with `Cache-Control: no-store` except successful leave (204).

| Method | Path | Input | Outcome |
|---|---|---|---|
| GET | `/healthz` | none | `{ "status": "ok" }` |
| POST | `/v1/rooms` | `{}` | 201: credentials for new host, including `inviteCode` |
| POST | `/v1/rooms/{roomId}/join` | `{ "inviteCode": "..." }` | 201: member credentials |
| GET | `/v1/rooms/{roomId}` | `Authorization: Bearer <sessionToken>` | snapshot of members and membership revision |
| DELETE | `/v1/rooms/{roomId}/members/me` | `Authorization: Bearer <sessionToken>` | 204; host leave destroys the room |

The server generates cryptographically random identifiers, invitation codes, and 24-hour ephemeral session tokens. The room holds at most **five** members including the host, guarded by a mutex. State is in memory only and is discarded on server restart. Session expiration is enforced lazily on access and old rooms are reaped during creation. At most 1024 rooms may exist concurrently. The API is intentionally not exposed on non-loopback interfaces without TLS. The room snapshot never contains invitation codes or session tokens.

This prototype has **no playback state, clock synchronization, readiness, scheduling, member reconnection identity, session renewal, durable audit trail, or network rate limiting**. Repeated joins create separate members until the capacity is reached. None of these limitations may be presented as completed features.

## Future protocol design requirements

Before cross-network release, define versioned WSS messages, explicit session identity and resume credentials, roomEpoch/clockEpoch/stateRevision, trackGeneration, track identity, clock sampling, prepare/arm/commit/cancel rules, drift correction, time unit/range handling, and tested authorization boundaries. Never serialize media credentials, local file paths, or signed media URLs into room control state.
