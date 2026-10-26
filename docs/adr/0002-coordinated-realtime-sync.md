# ADR-0002: server-coordinated WebSocket synchronization prototype

**Status:** provisional, pending multi-device audio verification.

## Decision

Extend the existing Go room coordinator with a restricted WebSocket message protocol on the same HTTP(S) listener. For non-loopback binds, require TLS and use WSS. Clients authenticate by sending the ephemeral session token in the first WebSocket frame, never in a URL. No audio or private local file paths flow through this connection. The client handles permitted local audio; server controls timeline, role authorization, identity generation, readiness and scheduled commands.

Retain first-party Go standard-library dependencies only for the prototype. The narrow WebSocket implementation accepts bounded masked text frames, rejects fragmentation, rejects browser cross-origin connections and does not implement compression. Before a public release, prefer auditing/upgrading to a well-maintained library or independently validating RFC 6455 and TLS termination, and implement robust abuse limits.

## Rationale

Client-to-server outbound TLS works across NAT/firewall boundaries when both clients can reach the same public server. Attempting full P2P/ICE/STUN/TURN is unnecessary for the first proof of synchronized *local* audio playback, and would add significantly more security and network complexity. A single server monotonic clock domain and authoritative state revision give a measurable, testable time anchor.

## Known gaps

Not sample-accurate; not acoustic synchronized-output proof. No ARM/ACK/COMMIT consensus, no buffer-delay measurement, no drift-rate estimator, and only bounded step-seek correction. JavaScript UI timers have jitter. Clock estimates assume limited RTT/asymmetry and expire after 60 seconds. New server process resets all sessions and clock epoch. A remote public WSS deployment must receive additional abuse/DoS and operations/security review. Next milestone is a Qt SDK build and two *physical* devices with loopback/recorded audio measurements.
