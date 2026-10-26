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
