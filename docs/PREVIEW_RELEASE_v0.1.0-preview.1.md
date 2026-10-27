# Spiral v0.1.0-preview.1 — Windows x64 test preview

> **Experimental, unsigned test build. NOT a stable or production release.**
> The repository's [Issue #11](https://github.com/Grymoira/Spiral/issues/11)
> remains open: two-device acoustic synchronization and fail-closed playback
> have **not** passed real hardware acceptance.

This first **Windows 11 / Windows 10 x64** test preview bundles the Spiral Qt
desktop client with its dynamically linked Qt runtime and a local Go
coordinator. It exists so testers can run the actual multimedia backend and
report reproducible results instead of relying on CI simulations.

## Windows setup (no Microsoft Store required)

1. Download `Spiral-v0.1.0-preview.1-windows-x64.zip` and the adjacent
   `.sha256` file from the GitHub Release assets.
2. In PowerShell, in the download folder, verify:
   ```powershell
   (Get-FileHash .\Spiral-v0.1.0-preview.1-windows-x64.zip -Algorithm SHA256).Hash
   ```
   Compare the result with the hexadecimal value in the `.sha256` file.
3. Extract the entire ZIP to a writable folder, preserving the `platforms`,
   `qml`, `multimedia` and other plugin subdirectories. Do **not** run the
   EXE from inside the ZIP.
4. Install the official **Microsoft Visual C++ 2015–2022 Redistributable
   (x64)** if it is not already present. This distribution intentionally
   does not bundle Microsoft's compiler runtime libraries. Use Microsoft's
   official installer, not third-party DLL websites.
5. Open PowerShell inside the extracted `Spiral-v0.1.0-preview.1-windows-x64`
   directory and run:
   ```powershell
   .\spiral-server.exe -listen 127.0.0.1:8787
   ```
   Keep this terminal running. The coordinator is bound **only to this
   computer**, not to the public Internet.
6. In the same directory, double-click `spiral-client.exe`. Keep the default
   address `http://127.0.0.1:8787`, select a locally held **authorized test
   WAV file**, create a room and share that file's hash. A second local
   instance can join using the invitation to test the basic room protocol.
   Each client must select the **identical** file locally before becoming Ready.

**For genuine tests on separate computers/networks**, the clients must connect
to one reachable authenticated HTTPS/WSS coordinator with a properly trusted
TLS certificate. A loopback-only server at `127.0.0.1` cannot coordinate
other computers; do not rebind a plaintext server to `0.0.0.0`. Public
deployment is still experimental, security hardening is unfinished, and it is
not recommended to expose this technical preview directly on the Internet.

## What's included

- `spiral-client.exe`: experimental Qt 6.8.3 Quick/Multimedia/WebSockets
  client and deployed shared Qt/QML/plugin dependencies.
- `spiral-server.exe`: standalone Go coordinator executable, defaulting to
  loopback HTTP/WS and requiring certificate/key for non-loopback TLS.
- `README-PREVIEW.md`, `BUILD-INFO.txt`, MIT source license, third-party
  notices and LGPL license text for dynamically linked Qt components.
- No bundled audio recordings, accounts, tokens, scraped media or provider
  credentials.

## Evidence and known limitations

- Windows package CI must compile with MSVC 2022 x64, pass Go and source-derived
  QML tests, Qt native CTests and synthetic WAV analyzer tests, run the
  **packaged** Go health endpoint and check that the deployed Qt desktop
  process remains alive for a short startup interval **without a Qt SDK on
  PATH**. These are software gates, *not a clean Windows install, an
  interactive GUI test, or a measurement of actual sound output*.
- **Not tested:** output devices and codecs on your Windows computer, dual
  physical device onset/stop accuracy, actual sleep/wake, hard network
  partitions, cancellation delivery under network failure, long-run
  stability, and Internet deployment load/security.
- Timing promises, including P50/P95/max acoustic thresholds, are **not
  established**; a reported Qt player position is not a DAC timestamp.
- Windows package is unsigned and non-notarized; Windows SmartScreen may
  warn. Always verify the archive checksum and source. No installer/MSIX
  or automatic updater is provided.
- macOS and Linux distributable desktop packages are not yet available;
  packaging acceptance [Issue #12](https://github.com/Grymoira/Spiral/issues/12)
  remains open. Source-build guidance is in the repository README.
- Qt and Qt's bundled multimedia/codec dependencies have their respective
  upstream licenses; this preview uses dynamically linked Qt binaries
  supplied by Qt's distribution. For component licensing and source access
  consult [Qt open-source licensing](https://www.qt.io/licensing/open-source-lgpl-obligations),
  [Qt source](https://code.qt.io/cgit/qt/),
  [FFmpeg licensing](https://ffmpeg.org/legal.html), and the included
  license/notice documents. Spiral's MIT license does not cover third parties.

## Useful test reports

Record OS edition and build, CPU architecture, playback/output device, audio
file codec/duration, Qt backend error messages (without secrets), local versus
TLS network topology, command sequence, reconnect/suspend times and exact
`BUILD-INFO.txt` source SHA. For synchronization, retain **actual
common-clock stereo recordings** and report sample counts and independently
chosen limits. See
[`tests/ACOUSTIC_ACCEPTANCE.md`](https://github.com/Grymoira/Spiral/blob/main/tests/ACOUSTIC_ACCEPTANCE.md).

Report findings as a new GitHub Issue with steps, expected behavior, observed
behavior, and redacted logs; do not post session tokens, invitation codes,
recording paths, public IPs or private recordings.
