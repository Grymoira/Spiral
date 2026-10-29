# Issue #11: annotated acoustic start/stop transitions (partial instrumentation)

`audio_alignment.py` remains the only supported **uninterrupted click-train**
analyzer. `audio_transitions.py` is a separate, stdlib-only utility for
start/stop edges in a **continuously audible reference signal** (for example,
an original constant test tone). Do not feed it the quiet-spaced click track
and interpret the gaps as pause events.

Record two physically isolated outputs simultaneously in one stereo PCM16 WAV,
using a shared sample clock. Retain the original WAV, audio file hash,
channel-isolation calibration, logs and network impairment timeline. Create a
separate annotation JSON from reference instants placed on that **same
recorder timebase**. Timestamps copied from the Qt player's wall clock,
`player.position`, or the server clock are not valid sample-clock anchors.
For credible control-to-sound latency, use a capture-visible hardware trigger
or independently calibrated timecode, and document the method and accuracy.
Unverified manual annotations alone cannot establish absolute control latency.

Example manifest (times refer to the *capture* beginning):

```json
{
  "timebase": "capture-wav-ms",
  "annotation_method": "calibrated recorder-visible control trigger",
  "events": [
    {"id": "play-01", "kind": "start", "reference_ms": 1000, "window_ms": 400},
    {"id": "pause-01", "kind": "stop", "reference_ms": 3000, "window_ms": 400}
  ]
}
```

Preconditions: the 30 ms before a `start` is quiet; the signal is audible
immediately before a `stop`; each non-overlapping 300–5000 ms window contains
exactly the intended event. Provide at least 30 valid annotated events per
included kind for a real-capture bounded verdict. A detector checks for sustained activity/silence and rejects clipped,
missing or inconsistent events. **Starts require threshold-level activity in
at least 60% of a 20 ms confirmation window**; a short click-like transient
must not count as the onset of the continuous reference signal.
**Every subsequent complete 20 ms block through the declared start-event
window must also satisfy the 60% activity requirement**; an initial pulse
followed by silence makes the event `INCOMPLETE` instead of a valid start.
Stops require at least 10 ms of sustained silence. If a valid waveform falls below the
activity threshold, calibrate the input or use a justified threshold setting;
never silently relax the criterion to improve measured results.
The reported right-minus-left latency compares acoustic transitions within
one recording, **not** two OS clocks. A `stop` detection needs at least 10 ms
of quiet confirmation; its reported timestamp is the *beginning* of that
verified quiet period. Keep sample rate, threshold and geometry stable.

For explicitly *predeclared* reviewer-agreed limits (illustrative numbers
below are **not approved** acceptance thresholds):

```bash
python3 tools/audio_transitions.py \
  --input recorded-stereo.wav --events capture-events.json \
  --source real --capture-method isolated-stereo-mics \
  --p50-limit-ms 25 --p95-limit-ms 60 --max-limit-ms 100 \
  --output transitions-report.json
```

`--capture-method isolated-stereo-line` is for electrical output, not airborne
sound. `--source synthetic --capture-method synthetic` is only for fixtures.
`INCOMPLETE` includes missing transitions, clipping, and real groups with
fewer than 30 events. Missing limits yield `MEASURED_NO_PREDECLARED_LIMITS`.
Even `WITHIN_CONFIGURED_LIMITS` is **one capture's qualified measurement**;
it never automatically closes #11. Evidence provenance and microphone bleed
are not verified by software; neither seek correctness, track identity,
network safety, actual hardware silence after a partition nor OS suspend is
proved by detected transitions. Seek/track tests must additionally establish
that the *correct new media content and generation* resumed. Retain original
capture and independent fault-injection records for reviewer sign-off.

Run deterministic **synthetic-only** coverage with:

```bash
python3 -m unittest discover -v -s tools/tests -p 'test_*.py'
```
