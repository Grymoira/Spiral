#!/usr/bin/env python3
"""Offline click-track generation and paired-stereo acoustic alignment.

Requires only the Python standard library. This tool never contacts the
network and never infers audio timing from Qt MediaPlayer.position.
"""
from __future__ import annotations

import argparse
from array import array
from datetime import datetime, timezone
import json
import math
from pathlib import Path
import statistics
import sys
import wave

MAX_RECORD_SECONDS = 600
MIN_REAL_EVENTS = 30


def generate_click_track(
    output: Path, *, clicks: int = 40, spacing_ms: int = 1500,
    sample_rate: int = 48000,
) -> None:
    if not 2 <= clicks <= 300 or not 600 <= spacing_ms <= 10000:
        raise ValueError("click count must be 2..300; spacing must be 600..10000 ms")
    if sample_rate not in (44100, 48000, 96000):
        raise ValueError("sample rate must be 44100, 48000 or 96000")
    pulse_samples = round(0.02 * sample_rate)
    first_click = sample_rate  # one second of lead-in silence
    step = round(spacing_ms * sample_rate / 1000)
    frames = first_click + (clicks - 1) * step + pulse_samples + sample_rate
    if frames / sample_rate > MAX_RECORD_SECONDS:
        raise ValueError("generated track exceeds 600 seconds")
    output.parent.mkdir(parents=True, exist_ok=True)
    with wave.open(str(output), "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(sample_rate)
        for start in range(0, frames, 8192):
            samples = array("h")
            for i in range(start, min(frames, start + 8192)):
                offset = i - first_click
                phase = offset % step if offset >= 0 else pulse_samples
                if 0 <= phase < pulse_samples:
                    # A short, windowed 1.2 kHz tone. Short fades avoid hard
                    # clipping and make a detectable acoustic onset marker.
                    ramp = min(1.0, phase / (0.001 * sample_rate),
                               (pulse_samples - phase) / (0.001 * sample_rate))
                    level = 0.7 * ramp * math.sin(2 * math.pi * 1200 * phase / sample_rate)
                    samples.append(round(32767 * level))
                else:
                    samples.append(0)
            if sys.byteorder != "little":
                samples.byteswap()
            wav.writeframesraw(samples.tobytes())


def detect_onsets(
    input_path: Path, *, threshold: float, refractory_ms: float
) -> tuple[int, float, list[float], list[float], int]:
    if not 0.01 <= threshold <= 0.8:
        raise ValueError("threshold must be between 0.01 and 0.8 full scale")
    if not 100 <= refractory_ms <= 5000:
        raise ValueError("refractory-ms must be 100..5000")
    with wave.open(str(input_path), "rb") as wav:
        rate, channels, width = wav.getframerate(), wav.getnchannels(), wav.getsampwidth()
        frames = wav.getnframes()
        if channels != 2 or width != 2 or rate not in (44100, 48000, 96000) or wav.getcomptype() != "NONE":
            raise ValueError("capture must be uncompressed stereo PCM16 WAV at 44100, 48000 or 96000 Hz")
        if frames / rate > MAX_RECORD_SECONDS:
            raise ValueError("capture exceeds 600 seconds; split into shorter scenarios")
        threshold_sample = round(threshold * 32767)
        refractory = round(refractory_ms * rate / 1000)
        release = max(1, round(0.005 * rate))
        onsets: list[list[int]] = [[], []]
        armed = [True, True]
        below = [release, release]
        last = [-refractory - 1, -refractory - 1]
        clipped = 0
        index = 0
        while True:
            raw = wav.readframes(8192)
            if not raw:
                break
            values = array("h")
            values.frombytes(raw)
            if sys.byteorder != "little":
                values.byteswap()
            for offset in range(0, len(values), 2):
                for ch in (0, 1):
                    magnitude = abs(int(values[offset + ch]))
                    if magnitude >= 32760:
                        clipped += 1
                    if magnitude >= threshold_sample:
                        if armed[ch] and index - last[ch] >= refractory:
                            onsets[ch].append(index)
                            last[ch] = index
                        armed[ch] = False
                        below[ch] = 0
                    else:
                        below[ch] += 1
                        if below[ch] >= release:
                            armed[ch] = True
                index += 1
        return (
            rate, frames / rate,
            [x / rate for x in onsets[0]],
            [x / rate for x in onsets[1]], clipped,
        )


def pair_onsets(left: list[float], right: list[float], window_ms: float) -> list[float]:
    if not 5 <= window_ms <= 500:
        raise ValueError("pair-window-ms must be 5..500")
    window = window_ms / 1000
    i = j = 0
    differences: list[float] = []
    while i < len(left) and j < len(right):
        d = right[j] - left[i]
        if abs(d) <= window:
            differences.append(d * 1000)
            i += 1
            j += 1
        elif right[j] > left[i]:
            i += 1
        else:
            j += 1
    return differences


def percentile(values: list[float], quantile: float) -> float:
    ordered = sorted(values)
    if not ordered:
        raise ValueError("percentiles require samples")
    location = (len(ordered) - 1) * quantile
    low = math.floor(location)
    fraction = location - low
    high = min(low + 1, len(ordered) - 1)
    return ordered[low] * (1 - fraction) + ordered[high] * fraction


def analyze(
    input_path: Path, *, expected_clicks: int, source_claim: str,
    capture_method: str, threshold: float = 0.12,
    refractory_ms: float = 400, pair_window_ms: float = 150,
    limits: tuple[float, float, float] | None = None,
) -> dict:
    if not 2 <= expected_clicks <= 300:
        raise ValueError("expected-clicks must be 2..300")
    if source_claim not in ("real", "synthetic"):
        raise ValueError("source must be 'real' or 'synthetic'")
    if capture_method not in ("isolated-stereo-mics", "isolated-stereo-line", "synthetic"):
        raise ValueError("unknown capture method")
    if source_claim == "real" and capture_method == "synthetic":
        raise ValueError("real source cannot use a synthetic capture method")
    if source_claim == "synthetic" and capture_method != "synthetic":
        raise ValueError("synthetic tests must use synthetic capture method")
    if limits is not None and (any(x <= 0 or not math.isfinite(x) for x in limits)
                                or not limits[0] <= limits[1] <= limits[2]):
        raise ValueError("positive limits must be ordered P50 <= P95 <= max")
    rate, duration, left, right, clipped = detect_onsets(
        input_path, threshold=threshold, refractory_ms=refractory_ms)
    deltas = pair_onsets(left, right, pair_window_ms)
    abs_deltas = [abs(d) for d in deltas]
    complete = (len(left) == expected_clicks and len(right) == expected_clicks
                and len(deltas) == expected_clicks and clipped == 0)
    warnings: list[str] = []
    if clipped:
        warnings.append("Clipped PCM samples invalidate reliable onset timing")
    if not complete:
        warnings.append("Missing, extra or unpaired clicks: no full acceptance claim")
    if source_claim == "real" and expected_clicks < MIN_REAL_EVENTS:
        complete = False
        warnings.append("Real acoustic acceptance requires at least 30 paired events per scenario")
    if source_claim == "synthetic":
        warnings.append("Synthetic capture is a unit test, NEVER physical acoustic evidence")
    measures = None
    raw_limits_values = None
    if deltas:
        raw_limits_values = (percentile(abs_deltas, 0.50),
                             percentile(abs_deltas, 0.95), max(abs_deltas))
        measures = {
            "p50_absolute_ms": round(raw_limits_values[0], 3),
            "p95_absolute_ms": round(raw_limits_values[1], 3),
            "max_absolute_ms": round(raw_limits_values[2], 3),
            "median_signed_right_minus_left_ms": round(statistics.median(deltas), 3),
        }
    verdict = "INCOMPLETE"
    if complete:
        if limits is None:
            verdict = "MEASURED_NO_PREDECLARED_LIMITS"
        else:
            # Compare unrounded measurements: formatting must never turn a
            # genuine threshold exceedance into a favorable verdict.
            within = all(measured <= bound for measured, bound in
                         zip(raw_limits_values, limits))
            verdict = "WITHIN_CONFIGURED_LIMITS" if within else "OUTSIDE_CONFIGURED_LIMITS"
    return {
        "schema_version": 1,
        "tool": "spiral-offline-acoustic-click-alignment",
        "generated_at_utc": datetime.now(timezone.utc).isoformat(),
        "source_claim_unverified": source_claim,
        "capture_method_claim_unverified": capture_method,
        "scope": "one shared-stereo-recording session; NOT Issue #11 overall acceptance",
        "sample_rate_hz": rate,
        "duration_seconds": round(duration, 3),
        "expected_clicks": expected_clicks,
        "detected_clicks": {"left": len(left), "right": len(right), "paired": len(deltas)},
        "unmatched": {"left": len(left) - len(deltas), "right": len(right) - len(deltas)},
        "clipped_samples": clipped,
        "threshold_fraction": threshold,
        "pair_window_ms": pair_window_ms,
        "measurements": measures,
        "signed_offsets_ms": [round(x, 3) for x in deltas],
        "limits_ms": ({"p50": limits[0], "p95": limits[1], "max": limits[2]}
                      if limits is not None else None),
        "verdict": verdict,
        "warnings": warnings,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    generator = commands.add_parser("generate", help="create identical mono marker track")
    generator.add_argument("--output", type=Path, required=True)
    generator.add_argument("--clicks", type=int, default=40)
    generator.add_argument("--spacing-ms", type=int, default=1500)
    generator.add_argument("--sample-rate", type=int, default=48000)

    evaluator = commands.add_parser("analyze", help="analyze a single stereo recording")
    evaluator.add_argument("--input", type=Path, required=True)
    evaluator.add_argument("--output", type=Path, help="optional JSON report output")
    evaluator.add_argument("--expected-clicks", type=int, required=True)
    evaluator.add_argument("--source", choices=("real", "synthetic"), required=True)
    evaluator.add_argument("--capture-method", choices=(
        "isolated-stereo-mics", "isolated-stereo-line", "synthetic"), required=True)
    evaluator.add_argument("--threshold", type=float, default=0.12)
    evaluator.add_argument("--refractory-ms", type=float, default=400)
    evaluator.add_argument("--pair-window-ms", type=float, default=150)
    evaluator.add_argument("--p50-limit-ms", type=float)
    evaluator.add_argument("--p95-limit-ms", type=float)
    evaluator.add_argument("--max-limit-ms", type=float)
    args = parser.parse_args(argv)
    try:
        if args.command == "generate":
            generate_click_track(args.output, clicks=args.clicks,
                                 spacing_ms=args.spacing_ms, sample_rate=args.sample_rate)
            print(f"Generated mono marker WAV with {args.clicks} clicks at {args.output}")
            return 0
        provided = (args.p50_limit_ms, args.p95_limit_ms, args.max_limit_ms)
        if any(x is not None for x in provided) and not all(x is not None for x in provided):
            parser.error("all three P50/P95/max limits are required for a bounded verdict")
        result = analyze(
            args.input, expected_clicks=args.expected_clicks, source_claim=args.source,
            capture_method=args.capture_method, threshold=args.threshold,
            refractory_ms=args.refractory_ms, pair_window_ms=args.pair_window_ms,
            limits=provided if all(x is not None for x in provided) else None,
        )
        encoded = json.dumps(result, indent=2, ensure_ascii=False) + "\n"
        if args.output is not None:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(encoded, encoding="utf-8")
        print(encoded, end="")
        return 2 if result["verdict"] in ("INCOMPLETE", "OUTSIDE_CONFIGURED_LIMITS") else 0
    except (ValueError, OSError, wave.Error) as exc:
        parser.exit(2, f"audio alignment error: {exc}\n")


if __name__ == "__main__":
    raise SystemExit(main())
