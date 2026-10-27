# Test coverage

The currently executable automated tests are under `server/internal/{rooms,api}`. Run `cd server && go test -race -count=1 ./...`, or use the Qt-free CMake preset and `ctest --preset server-only`.

A CTest pass only verifies the Go baseline. It does **not** validate the Qt player, remote WebSocket transport, synchronized playback, real-device acoustic output, or any release-quality timing targets.

The automated Go tests cover: room lifecycle, authentication, atomic five-member capacity,
expiration cleanup, listener address safety, strict JSON parsing (including duplicate keys),
HTTP method semantics, and response privacy headers. A real-process loopback HTTP smoke
check can additionally exercise room creation, joining, snapshots, and departure.

The Qt desktop module is **not** compiled by the `server-only` preset. CMake refuses
to configure a selected server component without Go, and refuses a configuration with
all components disabled; a successful no-op build is not considered an acceptance test.

## Phase 6: suspend/wake regression evidence

The Qt-enabled CMake preset now builds `spiral-clock-continuity-test`, a native
QtCore CTest binary that uses synthetic monotonic and wall timestamps. It
verifies normal elapsed time, a sleep-like monotonic/wall divergence, backward
clock changes and the recovery baseline. This must not be interpreted as a
literal operating-system suspend test.

The desktop CI additionally executes `client/tests/sleep_resync.test.cjs`,
which runs functions extracted directly from the shipping QML with a clock
discontinuity injected immediately before a delayed playback timer or time
reply. It requires immediate pause/Ready withdrawal, canceled stale timers,
and a correlated authoritative snapshot before Ready may be restored.

The Go integration `TestAuthenticatedSnapshotResyncReturnsCurrentRevisionAndRequestID`
exercises authenticated snapshot reads with an actual WebSocket connection.
These automated tests cannot measure audio output at speakers. Hardware
suspend/resume, a real network partition and two-device acoustic captures
remain separate release-blocking evidence under Issue #11.
