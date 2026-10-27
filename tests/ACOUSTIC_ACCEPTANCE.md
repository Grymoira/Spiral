# Issue #11 — Physical two-device acoustic validation protocol

**Status:** procedure and measurement tooling available; **no physical measurements
have been made by this repository's automated tests**. Do not close #11 using
synthetic fixtures, player.position, clock-probe RTT, or a successful build.

## 1. Equipment and evidence

Use two **physical Qt Spiral desktop clients** playing the **same locally copied
audio file** while each has network access to the same authenticated coordinator.
For an initial reproducible test they can occupy the **same location but connect
through different networks** (e.g., independent hotspots); this preserves the
application's across-network scenario while allowing *one recording clock*.

Capture both outputs **simultaneously on one two-channel PCM recorder**:

- For acoustic output, aim isolated microphones at each device's loudspeaker,
  match speaker-to-microphone distances, minimize bleed/reflections, and route
  microphone A to the recorder's **left** channel, B to **right**. Check each
  channel separately: if B's click clearly appears on A's microphone (or vice
  versa), the channel-isolation assumption is invalid and the recording must be
  rejected or the geometry changed.
- For electrical timing, route independent, legally accessible analog line
  outputs into isolated left/right inputs **without mixing or cross-feeding**.
  This measures electrical output arrival, **not airborne acoustic delay**.
- Capture **one uncompressed 16-bit stereo WAV** at 44.1, 48 or 96 kHz.
  Disable auto-gain, echo cancellation, noise suppression, compression and
  monitoring feedback; check headroom and avoid clipping.
- A single 2-channel recording shares one sample clock. **Two independent WAV
  recordings from two independent laptops do not**: aligning their file headers
  or OS timestamps cannot establish sub-100ms acoustic differences. Truly
  distant endpoints need additional independently calibrated timing hardware
  (for example GPS-disciplined recording); this tool does not validate that.

Record hardware/OS versions, Qt and server commit, network type, audio backend,
speaker and microphone paths/distances, recorder settings, clock-probe quality,
room size, test date, test duration, and fault-injection conditions. Never put
tokens, proxy credentials or private media paths in a shared report.

## 2. Choose limits **before** the recording

Write down three **project-approved** upper bounds for absolute left/right
acoustic onset error: `P50`, `P95`, and `max`, in milliseconds, satisfying
`P50 <= P95 <= max`. Do not choose or change them after seeing data. No
thresholds have yet been formally accepted for Issue #11; passing arbitrary
example values is **not** final acceptance.

For statistically meaningful per-scenario percentiles, the analyzer requires
at least **30 paired events** for a recording declared real; the default
marker track has **40**. Repeat on each required OS/audio-backend configuration
and at multiple network conditions. Report the sample count, missing events,
signed bias and absolute P50/P95/max.

## 3. Generate a reference click track

At the repository root, with Python 3 (stdlib only):

```bash
python3 tools/audio_alignment.py generate --output spiral-clicks.wav --clicks 40 --spacing-ms 1500
```

Copy **identical bytes** to both clients (the project already compares their
SHA-256). The track is 48 kHz mono PCM16 with a 1 s quiet lead-in and short,
well-spaced 1.2 kHz audible click markers. Set identical, safe speaker volume.
Use Spiral's normal host/join, load, Ready, ARM/ACK/COMMIT process; record
both physical sound outputs on one stereo capture device.

Recording needs enough lead-in, the whole click train and a quiet tail.
Check channel isolation by playing each device *alone* for a brief setup
recording; these calibration recordings are not counted as sync samples.
For repeated play trials, reset both players and acquire a **new** paired
capture; do not concatenate unrelated recordings with independent timelines.

## 4. Analyze one physically recorded WAV

Declare the pre-approved limits explicitly. The numbers below are
**illustrative CLI examples, not approved Issue #11 thresholds**:

```bash
python3 tools/audio_alignment.py analyze \
  --input captured-stereo.wav \
  --source real \
  --capture-method isolated-stereo-mics \
  --expected-clicks 40 \
  --p50-limit-ms 25 --p95-limit-ms 60 --max-limit-ms 100 \
  --output acoustic-report.json
```

For analog electrical measurements instead use
`--capture-method isolated-stereo-line`. Tool outputs only offline local
JSON: frame/sample rate, channel counts, paired and unmatched click counts,
sample-by-sample signed right-minus-left offsets, absolute P50/P95/max,
thresholds, warning flags, and an **individual recording** verdict.

An incomplete capture (missing/extra/unpaired clicks, any clipping, fewer
than 30 real events) returns `INCOMPLETE` with a nonzero exit status. An
out-of-bounds recording returns `OUTSIDE_CONFIGURED_LIMITS`, also nonzero.
Without all three predeclared limits, the verdict is
`MEASURED_NO_PREDECLARED_LIMITS`, **not PASS**. A within-limits result only
means **this one declared capture** met the supplied values; it does not
prove authenticity, channel isolation, any other platform, or #11 overall.

If the onset detector misses a quiet marker, investigate input levels and
noise before adjusting `--threshold` (default 0.12 of full scale). If the
onsets are more than `--pair-window-ms` apart (default 150 ms), they are
**unmatched**, not silently assigned a favorable error value. Never hide
unpaired clicks to improve percentile results.

## 5. Required scenario matrix

For every relevant desktop OS/audio backend and two-network arrangement,
repeat the physical capture and retain raw WAVs and reports:

| Scenario | Required checks | Evidence |
| --- | --- | --- |
| Clean play/start | ARM/ACK/COMMIT; 40 paired clicks; repeated trials | Acoustic onset P50/P95/max and all raw captures |
| Pause | Both channels stop; no free-running residual sound or cancellation replay | Continuous recording around PAUSE, annotated event time and audible tails |
| Seek | New position is reached; no earlier buffered segment leaks; restart click alignment | Capture before/after seek and explicit audible timeline markers |
| Track change/reload | Previous track is silent; readiness resets; new track only starts with valid generation | Two distinct reference tracks and separate labeled captures |
| Lost ACK / cancellation | No unauthorized audible start after failed barrier | Network impairment trace + sound capture of silence or guarded recovery |
| Disconnect/reconnect | No late playback; guard pauses pending current state until fresh authority | Disconnect timestamps, app logs, recorded silence/onset |
| Media stall/fault | Immediate local pause, Ready(false), no autoplay after buffering returns | Controlled media faults + playback/device audio capture |
| Suspend/wake | No old COMMIT executed; never join current playing state without fresh confirmation | Actual OS suspend and resume times + audio capture |

**Important limitation:** The supplied click detector measures timing of an
**uninterrupted, expected click sequence**. A pause, seek, track change, or
disconnection can legitimately omit markers: the strict detector then
returns `INCOMPLETE`. That does not automatically mean a synchronization
defect, and the tool must **not** be reused to claim pause/seek latency.
Those scenarios require continuous recorded waveforms with annotated,
independently measured control timestamps and separate segment-by-segment
onset analysis or a future purpose-built timing instrument. Track
`INCOMPLETE` separately from genuine failure.

## 6. Synthetic regression (NOT hardware evidence)

```bash
python3 -m unittest discover -v -s tools/tests -p 'test_*.py'
```

These fixtures generate artificial left/right waveform offsets, a dropped
right-channel event and clipped samples to confirm detector behavior. They
provide **zero** evidence of real Qt Multimedia audio output or two-device
acoustic synchronization.

**Release gate:** Issue #11 remains OPEN until signed-off thresholds, raw
independently obtained **physical audio recordings**, scenario reports,
validated hardware setup, fault-injection evidence and a reviewer-accepted
summary exist. No CI green check substitutes for that acceptance.

## 7. Manual host removal safety case (Issue #11, phase 9)

After normal synchronized playback and a deliberately blackholed guest
connection, induce an unacknowledged cancellation. Observe that a new play
is rejected with `cancellation_pending` while the guest remains a member.
Then manually remove that guest through the host UI. Preserve both machines'
continuous audio captures and authoritative server logs; verify that the
removed guest becomes audibly silent, records any excess sound tail and does
not replay its old COMMIT after network recovery. Rejoin as a **new authenticated
member** before any subsequent shared play and use a fresh ARM/ACK/COMMIT.

This is a distinct safety/availability test; the absence of guest snapshots or
presence of HTTP 204 **does not prove that its loudspeaker stopped**. Record
the actual guest stop latency, interrupted segment, environment, packet-loss
configuration and raw capture. Do not conflate the surviving host's new
one-member room alignment with the former two-member session.
