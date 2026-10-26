# Spiral engineering rules

- Treat this repository as an incremental desktop audio-sync project, not as a completed synchronized-player product.
- Read `docs/DEVELOPMENT_STATUS.md`, `docs/roadmap.md`, and applicable ADRs before changing behavior.
- Prefer small, tested, reversible commits. Never discard uncommitted work or rewrite history.
- Data integrity, access control, privacy, and test honesty outweigh UI polish and performance claims.
- Never send media URLs, local absolute paths, access tokens, or proxy credentials through room control messages or diagnostic logs.
- Do not enable arbitrary music scraping, DRM circumvention, or insecure TLS defaults.
- Preserve the five-member room limit (including host) and enforce it on the authoritative server under concurrent joins.
- Keep Qt UI code independent from room authority. Qt Multimedia position does not prove actual acoustic synchronization.
- Use C++20, Qt 6.8+, Go 1.23+, CMake, CTest, and Go's standard tooling unless an ADR explicitly approves another dependency.
- Run relevant tests and record exact failures or missing SDKs. Never claim unrun tests passed.
- Keep the prototype HTTP API restricted to verified loopback-bound sockets; any public deployment needs authenticated WSS/TLS, abuse limits, and a threat-model review.
- Reject ambiguous JSON objects and inconsistent API authorization before evolving the protocol.
- A selected build component must fail configuration when its required toolchain is absent; a zero-test run is not validation.
- Update `docs/DEVELOPMENT_STATUS.md` and roadmap whenever milestone scope changes.
