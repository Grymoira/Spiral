# Test coverage

The currently executable automated tests are under `server/internal/{rooms,api}`. Run `cd server && go test -race -count=1 ./...`, or use the Qt-free CMake preset and `ctest --preset server-only`.

A CTest pass only verifies the Go baseline. It does **not** validate the Qt player, remote WebSocket transport, synchronized playback, real-device acoustic output, or any release-quality timing targets.
