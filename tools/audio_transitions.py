#!/usr/bin/env python3
"""Measure annotated start/stop transitions in one shared-clock stereo PCM16 WAV.

Annotations MUST refer to recorder sample time, not remote Qt or server clocks.
This is an offline measurement aid, not proof of the annotation's provenance,
channel isolation, correct seek/track content, or Issue #11 acceptance.
"""
from __future__ import annotations

import argparse
from array import array
import json
import math
from pathlib import Path
import sys
import wave

MIN_REAL_EVENTS = 30


def percentile(values: list[float], quantile: float) -> float:
    ordered = sorted(values)
    if not ordered:
        raise ValueError('percentiles require samples')
    location = (len(ordered) - 1) * quantile
    low = math.floor(location)
    high = min(low + 1, len(ordered) - 1)
    part = location - low
    return ordered[low] * (1 - part) + ordered[high] * part

PRE_ROLL_MS = 30
CONFIRM_MS = 10
MAX_EVENTS = 500


def validate_limits(limits: tuple[float, float, float] | None) -> None:
    if limits is not None and (len(limits) != 3 or
        any(not math.isfinite(x) or x <= 0 for x in limits) or
        not limits[0] <= limits[1] <= limits[2]):
        raise ValueError('limits must be finite, positive and ordered P50 <= P95 <= max')


def find_transition(values: array, *, channel: int, rate: int, reference: int,
                    threshold: int, kind: str) -> int | None:
    """Return sample of verified onset or first sample of sustained silence."""
    confirm = max(1, round(CONFIRM_MS * rate / 1000))
    pre = max(1, round(20 * rate / 1000))
    above_before = sum(abs(values[2 * i + channel]) >= threshold
                       for i in range(reference - pre, reference))
    # An event is unmeasurable unless the precondition is unambiguous.
    if kind == 'start' and above_before:
        return None
    if kind == 'stop' and (above_before < pre // 4 or
        sum(abs(values[2 * i + channel]) >= threshold
            for i in range(reference - max(1, round(.003 * rate)), reference))
        < max(1, round(.003 * rate)) // 4):
        return None
    length = len(values) // 2
    if kind == 'start':
        for i in range(reference, length - confirm + 1):
            if abs(values[2 * i + channel]) < threshold:
                continue
            active = sum(abs(values[2 * k + channel]) >= threshold
                         for k in range(i, i + confirm))
            if active >= confirm // 4:
                return i
    else:
        for i in range(reference, length - confirm + 1):
            if all(abs(values[2 * k + channel]) < threshold
                   for k in range(i, i + confirm)):
                # A reappearance in the declared window invalidates a stop.
                if any(abs(values[2 * k + channel]) >= threshold
                       for k in range(i + confirm, length)):
                    return None
                return i
    return None


def analyze(input_path: Path, annotations_path: Path, *, source: str,
            capture_method: str, threshold: float = 0.12,
            limits: tuple[float, float, float] | None = None) -> dict:
    if source not in ('real', 'synthetic'):
        raise ValueError('source must be real or synthetic')
    if capture_method not in ('isolated-stereo-mics', 'isolated-stereo-line', 'synthetic'):
        raise ValueError('unsupported capture method')
    if (source == 'real') != (capture_method != 'synthetic'):
        raise ValueError('real/synthetic source and capture method disagree')
    if not 0.01 <= threshold <= 0.8 or not math.isfinite(threshold):
        raise ValueError('threshold must be 0.01..0.8 of full scale')
    validate_limits(limits)
    manifest = json.loads(annotations_path.read_text(encoding='utf-8'))
    if not isinstance(manifest, dict) or manifest.get('timebase') != 'capture-wav-ms':
        raise ValueError('manifest must declare timebase=capture-wav-ms')
    events = manifest.get('events')
    if not isinstance(events, list) or not 1 <= len(events) <= MAX_EVENTS:
        raise ValueError('events must be a nonempty list (up to 500)')
    # Annotations are operator claims, not independently verified trigger marks.
    if not isinstance(manifest.get('annotation_method'), str) or not manifest['annotation_method'].strip():
        raise ValueError('record an annotation_method for sample-time provenance')
    records: list[dict] = []
    raw_offsets: dict[str, list[float]] = {'start': [], 'stop': []}
    seen: set[str] = set()
    last_end = 0
    with wave.open(str(input_path), 'rb') as wav:
        rate = wav.getframerate()
        frame_count = wav.getnframes()
        if (wav.getnchannels() != 2 or wav.getsampwidth() != 2 or
            wav.getcomptype() != 'NONE' or rate not in (44100, 48000, 96000)):
            raise ValueError('input must be uncompressed stereo PCM16 WAV (44.1/48/96 kHz)')
        if frame_count / rate > 600:
            raise ValueError('capture exceeds 600 seconds')
        for e in events:
            if not isinstance(e, dict):
                raise ValueError('each event must be an object')
            name, kind = e.get('id'), e.get('kind')
            ref_ms, window_ms = e.get('reference_ms'), e.get('window_ms')
            if not isinstance(name, str) or not name.strip() or name in seen:
                raise ValueError('event IDs must be unique nonempty strings')
            if kind not in ('start', 'stop'):
                raise ValueError('event kind must be start or stop')
            if (type(ref_ms) not in (int, float) or not math.isfinite(ref_ms) or
                type(window_ms) not in (int, float) or not math.isfinite(window_ms) or
                not 300 <= window_ms <= 5000):
                raise ValueError('finite reference_ms and 300..5000 window_ms required')
            ref = round(ref_ms * rate / 1000)
            end = round((ref_ms + window_ms) * rate / 1000)
            begin = ref - round(PRE_ROLL_MS * rate / 1000)
            if begin < last_end or begin < 0 or end > frame_count:
                raise ValueError('events must be ordered, nonoverlapping and inside WAV')
            seen.add(name)
            last_end = end
            wav.setpos(begin)
            values = array('h')
            values.frombytes(wav.readframes(end - begin))
            if len(values) != 2 * (end - begin):
                raise ValueError('truncated PCM WAV data inside event window')
            if sys.byteorder != 'little':
                values.byteswap()
            clipped = sum(abs(v) >= 32760 for v in values)
            origin = ref - begin
            threshold_sample = round(threshold * 32767)
            positions = [find_transition(values, channel=ch, rate=rate,
                          reference=origin, threshold=threshold_sample, kind=kind)
                         for ch in (0, 1)]
            latencies = [round((p - origin) * 1000 / rate, 3) if p is not None else None
                         for p in positions]
            valid = clipped == 0 and all(p is not None for p in positions)
            if valid:
                raw_offsets[kind].append((positions[1] - positions[0]) * 1000 / rate)
            records.append({
                'id': name, 'kind': kind, 'reference_ms': ref_ms,
                'window_ms': window_ms, 'left_latency_ms': latencies[0],
                'right_latency_ms': latencies[1],
                'right_minus_left_ms': (round((positions[1] - positions[0]) * 1000 / rate, 3)
                                        if valid else None),
                'clipped_samples': clipped,
                'valid': valid,
            })
    summary = {}
    raw_statistics = {}
    complete = True
    for kind in ('start', 'stop'):
        subset = [x for x in records if x['kind'] == kind]
        invalid = sum(not x['valid'] for x in subset)
        differences = [abs(offset) for offset in raw_offsets[kind]]
        raw_statistics[kind] = ((percentile(differences, 0.50),
                                 percentile(differences, 0.95), max(differences))
                                if differences else None)
        summary[kind] = {
            'total': len(subset), 'valid': len(differences), 'invalid': invalid,
            'p50_absolute_ms': round(percentile(differences, 0.50), 3) if differences else None,
            'p95_absolute_ms': round(percentile(differences, 0.95), 3) if differences else None,
            'max_absolute_ms': round(max(differences), 3) if differences else None,
        }
        if subset and (invalid or (source == 'real' and len(subset) < MIN_REAL_EVENTS)):
            complete = False
    verdict = 'INCOMPLETE' if not complete else 'MEASURED_NO_PREDECLARED_LIMITS'
    if complete and limits is not None:
        inside = all(all(measure <= limit for measure, limit in
                         zip(raw_statistics[kind], limits))
                     for kind in ('start', 'stop') if summary[kind]['total'])
        verdict = 'WITHIN_CONFIGURED_LIMITS' if inside else 'OUTSIDE_CONFIGURED_LIMITS'
    return {
        'schema_version': 1,
        'tool': 'spiral-offline-annotated-audio-transitions',
        'scope': 'one shared-clock capture; not Issue #11 overall acceptance',
        'source_claim_unverified': source,
        'capture_method_claim_unverified': capture_method,
        'annotation_method_claim_unverified': manifest['annotation_method'],
        'timebase': 'capture-wav-ms', 'sample_rate_hz': rate,
        'threshold_fraction': threshold,
        'limits_ms': dict(zip(('p50', 'p95', 'max'), limits)) if limits else None,
        'events': records, 'summary_by_kind': summary, 'verdict': verdict,
        'warnings': [
            'Annotations and channel isolation are operator claims, not verified by this tool',
            'A detected start/stop does not prove correct track or seek position',
            'Synthetic tests are NEVER physical acoustic evidence' if source == 'synthetic'
            else 'Real-capture label is not independently authenticated',
        ],
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--events', type=Path, required=True,
                        help='JSON: {"timebase":"capture-wav-ms", "annotation_method":"...", '
                             '"events":[{"id":"...","kind":"start|stop",'
                             '"reference_ms":1000,"window_ms":500}]}')
    parser.add_argument('--source', choices=('real', 'synthetic'), required=True)
    parser.add_argument('--capture-method', choices=(
        'isolated-stereo-mics', 'isolated-stereo-line', 'synthetic'), required=True)
    parser.add_argument('--threshold', type=float, default=0.12)
    parser.add_argument('--p50-limit-ms', type=float)
    parser.add_argument('--p95-limit-ms', type=float)
    parser.add_argument('--max-limit-ms', type=float)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args(argv)
    bounds = (args.p50_limit_ms, args.p95_limit_ms, args.max_limit_ms)
    if any(x is not None for x in bounds) and not all(x is not None for x in bounds):
        parser.error('all three predeclared limits are required')
    try:
        result = analyze(args.input, args.events, source=args.source,
                         capture_method=args.capture_method,
                         threshold=args.threshold,
                         limits=bounds if all(x is not None for x in bounds) else None)
        output = json.dumps(result, indent=2, ensure_ascii=False) + '\n'
        if args.output:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(output, encoding='utf-8')
        print(output, end='')
        return 2 if result['verdict'] in ('INCOMPLETE', 'OUTSIDE_CONFIGURED_LIMITS') else 0
    except (OSError, ValueError, KeyError, wave.Error, json.JSONDecodeError) as exc:
        parser.exit(2, f'audio transition error: {exc}\n')


if __name__ == '__main__':
    raise SystemExit(main())
