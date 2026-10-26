# ADR 0001 — Loopback HTTP bootstrap instead of premature public WSS

- Status: Accepted for development baseline only
- Date: 2026-10-26

## Context

The new Spiral repository contained only a README at inspection time. It had no working server or client and no approved synchronization wire contract. Implementing an Internet-facing websocket service before defining session authorization, clock domains and error semantics would be premature.

## Decision

Implement a deliberately limited **loopback-only** HTTP room membership API in Go's standard library to validate atomic capacity, authorization, cryptographic credential generation and development tooling. The API is not for remote operation and includes no playback state or media URLs. Qt local audio playback is an independent capability probe, not yet coupled to room creation.

The server rejects a non-loopback plaintext listen address. Sessions live only in process memory and expire after 24 hours. No tokens are logged by the server. Do not add permissive CORS or disable certificate verification.

## Consequences

The bootstrap is easy to build and has meaningful race-tested behavior without external Go dependencies. It cannot perform cross-network synchronization. A separate ADR and complete contract review are required before implementing authenticated WSS and public deployment, including rate limits, recovery semantics, transport security, privacy and clock uncertainty.

## Follow-up: loopback and JSON hardening (2026-10-26)

The listener also verifies the bound TCP IP before serving, rather than trusting a
`localhost` hostname or only the requested listen string. The JSON endpoints require
a single object and reject ambiguous duplicate top-level keys. This does not make
plain HTTP appropriate for network or Internet exposure.
