"""Synthetic-only regressions; does not supply physical Issue #11 evidence."""
from array import array
import json
from pathlib import Path
import sys
import tempfile
import unittest
import wave

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from tools.audio_transitions import analyze  # noqa: E402

RATE = 48000


def fixture(directory: Path, *, missing_right: bool = False,
            clipped: bool = False) -> tuple[Path, Path]:
    # 60 transitions, 30 onsets and 30 stops, with separately observable
    # left/right timing offsets. Avoid sharing computer-side player clocks.
    seconds = 36
    left = array('h', [0]) * (RATE * seconds)
    right = array('h', [0]) * (RATE * seconds)
    events = []
    for i in range(60):
        reference_ms = 500 + i * 550
        kind = 'start' if i % 2 == 0 else 'stop'
        events.append({'id': f'event-{i}', 'kind': kind,
                       'reference_ms': reference_ms, 'window_ms': 300})
        for ch, samples in enumerate((left, right)):
            if kind == 'start':
                on = round((reference_ms + (25 if ch == 0 else 42)) * RATE / 1000)
                off = round((reference_ms + 550 + (20 if ch == 0 else 37)) * RATE / 1000)
                if missing_right and i == 0 and ch == 1:
                    continue
                # A continuous square-wave fixture, crossing threshold on each cycle.
                for j in range(on, off):
                    samples[j] = 12000 if (j // 25) % 2 else -12000
    if clipped:
        left[round(.7 * RATE)] = 32767
    stereo = array('h')
    for i in range(len(left)):
        stereo.extend((left[i], right[i]))
    if sys.byteorder != 'little':
        stereo.byteswap()
    audio = directory / 'capture.wav'
    with wave.open(str(audio), 'wb') as wav:
        wav.setnchannels(2)
        wav.setsampwidth(2)
        wav.setframerate(RATE)
        wav.writeframes(stereo.tobytes())
    annotations = directory / 'events.json'
    annotations.write_text(json.dumps({
        'timebase': 'capture-wav-ms',
        'annotation_method': 'synthetic known sample offsets',
        'events': events,
    }), encoding='utf-8')
    return audio, annotations


class TransitionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.dir = Path(cls.tmp.name)
        cls.normal = fixture(cls.dir)

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def measure(self, paths=None, **options):
        audio, annotations = paths or self.normal
        return analyze(audio, annotations, source='synthetic',
                       capture_method='synthetic', limits=(20, 25, 30), **options)

    def test_annotated_start_and_stop_with_independent_sample_clock(self):
        result = self.measure()
        self.assertEqual(result['verdict'], 'WITHIN_CONFIGURED_LIMITS')
        self.assertEqual(result['summary_by_kind']['start']['valid'], 30)
        self.assertEqual(result['summary_by_kind']['stop']['valid'], 30)
        self.assertAlmostEqual(result['summary_by_kind']['start']['p50_absolute_ms'], 17, delta=1)
        self.assertAlmostEqual(result['summary_by_kind']['stop']['p50_absolute_ms'], 17, delta=1)
        self.assertTrue(any('NEVER physical' in w for w in result['warnings']))

    def test_strict_limits_fail(self):
        result = analyze(*self.normal, source='synthetic',
                         capture_method='synthetic', limits=(2, 5, 10))
        self.assertEqual(result['verdict'], 'OUTSIDE_CONFIGURED_LIMITS')

    def test_unrounded_limit_comparison_rejects_marginal_violation(self):
        result = analyze(*self.normal, source='synthetic',
                         capture_method='synthetic', limits=(16.999, 30, 30))
        self.assertEqual(result['verdict'], 'OUTSIDE_CONFIGURED_LIMITS')

    def test_missing_limits_never_pass(self):
        result = analyze(*self.normal, source='synthetic', capture_method='synthetic')
        self.assertEqual(result['verdict'], 'MEASURED_NO_PREDECLARED_LIMITS')

    def test_invalid_source_and_unbounded_threshold_rejected(self):
        with self.assertRaises(ValueError):
            self.measure(threshold=0)
        with self.assertRaises(ValueError):
            analyze(*self.normal, source='real', capture_method='synthetic')
        with self.assertRaises(ValueError):
            analyze(*self.normal, source='synthetic', capture_method='synthetic', limits=(10, 5, 20))

    def test_missing_channel_and_clipping_fail_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            data = fixture(Path(temp), missing_right=True)
            self.assertEqual(self.measure(data)['verdict'], 'INCOMPLETE')
            data = fixture(Path(temp), clipped=True)
            self.assertEqual(self.measure(data)['verdict'], 'INCOMPLETE')

    def test_manifest_rejects_overlaps_and_wrong_timebase(self):
        audio, annotations = self.normal
        manifest = json.loads(annotations.read_text())
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'events.json'
            manifest['timebase'] = 'qt-monotonic-ms'
            path.write_text(json.dumps(manifest))
            with self.assertRaises(ValueError):
                self.measure((audio, path))
            manifest['timebase'] = 'capture-wav-ms'
            manifest['events'][1]['reference_ms'] = manifest['events'][0]['reference_ms'] + 20
            path.write_text(json.dumps(manifest))
            with self.assertRaises(ValueError):
                self.measure((audio, path))

    def test_single_real_event_is_insufficient_for_acceptance(self):
        audio, annotations = self.normal
        manifest = json.loads(annotations.read_text())
        manifest['events'] = manifest['events'][:1]
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'events.json'
            path.write_text(json.dumps(manifest))
            result = analyze(audio, path, source='real',
                             capture_method='isolated-stereo-line', limits=(20, 25, 30))
        self.assertEqual(result['verdict'], 'INCOMPLETE')


if __name__ == '__main__':
    unittest.main()
