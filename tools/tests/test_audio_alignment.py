"""Deterministic, synthetic-only tests for the offline WAV alignment tool."""
from array import array
import math
from pathlib import Path
import sys
import tempfile
import unittest
import wave

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from tools.audio_alignment import (  # noqa: E402
    analyze, detect_onsets, generate_click_track, pair_onsets, percentile,
)

RATE = 48000


def stereo_fixture(path: Path, *, clicks: int = 40, delay_ms: float = 17.0,
                   missing_right: int | None = None, clipped: bool = False) -> None:
    spacing = round(0.7 * RATE)
    first = RATE // 2
    frames = first + clicks * spacing + RATE
    samples = array("h", [0]) * (frames * 2)
    pulse = round(0.02 * RATE)
    for n in range(clicks):
        left_start = first + n * spacing
        right_start = left_start + round((delay_ms + (n % 5 - 2)) * RATE / 1000)
        for ch, start in ((0, left_start), (1, right_start)):
            if ch == 1 and n == missing_right:
                continue
            for i in range(pulse):
                amp = round(14000 * math.sin(2 * math.pi * 1200 * i / RATE))
                samples[2 * (start + i) + ch] = amp
    if clipped:
        samples[2 * first] = 32767
    if sys.byteorder != "little":
        samples.byteswap()
    with wave.open(str(path), "wb") as wav:
        wav.setnchannels(2)
        wav.setsampwidth(2)
        wav.setframerate(RATE)
        wav.writeframes(samples.tobytes())


class AcousticHarnessTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.temp = tempfile.TemporaryDirectory()
        cls.directory = Path(cls.temp.name)
        cls.normal = cls.directory / "synthetic_stereo.wav"
        cls.missing = cls.directory / "missing.wav"
        cls.clipped = cls.directory / "clipped.wav"
        stereo_fixture(cls.normal)
        stereo_fixture(cls.missing, missing_right=5)
        stereo_fixture(cls.clipped, clipped=True)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.temp.cleanup()

    def measure(self, path: Path, **options) -> dict:
        return analyze(
            path, expected_clicks=40, source_claim="synthetic",
            capture_method="synthetic", limits=(20, 25, 30), **options
        )

    def test_known_17ms_relative_offset_with_jitter(self) -> None:
        report = self.measure(self.normal)
        self.assertEqual(report["detected_clicks"], {"left":40, "right":40, "paired":40})
        self.assertEqual(report["verdict"], "WITHIN_CONFIGURED_LIMITS")
        self.assertAlmostEqual(report["measurements"]["median_signed_right_minus_left_ms"],
                               17, delta=0.25)
        self.assertAlmostEqual(report["measurements"]["p50_absolute_ms"], 17, delta=0.25)
        self.assertLess(report["measurements"]["p95_absolute_ms"], 20)
        self.assertTrue(any("NEVER physical" in x for x in report["warnings"]))
        self.assertEqual(report["scope"], "one shared-stereo-recording session; NOT Issue #11 overall acceptance")

    def test_strict_limits_report_failure(self) -> None:
        report = analyze(self.normal, expected_clicks=40, source_claim="synthetic",
                         capture_method="synthetic", limits=(5, 8, 10))
        self.assertEqual(report["verdict"], "OUTSIDE_CONFIGURED_LIMITS")

    def test_missing_channel_click_prevents_acceptance(self) -> None:
        report = self.measure(self.missing)
        self.assertEqual(report["verdict"], "INCOMPLETE")
        self.assertEqual(report["detected_clicks"]["left"], 40)
        self.assertEqual(report["detected_clicks"]["right"], 39)
        self.assertEqual(report["detected_clicks"]["paired"], 39)
        self.assertEqual(report["unmatched"]["left"], 1)

    def test_clipping_rejects_nominally_aligned_sample(self) -> None:
        report = self.measure(self.clipped)
        self.assertEqual(report["verdict"], "INCOMPLETE")
        self.assertGreater(report["clipped_samples"], 0)

    def test_tight_pair_window_marks_unmatched_samples(self) -> None:
        report = self.measure(self.normal, pair_window_ms=5)
        self.assertEqual(report["verdict"], "INCOMPLETE")
        self.assertLess(report["detected_clicks"]["paired"], 40)

    def test_synthetic_source_cannot_claim_real_capture(self) -> None:
        with self.assertRaises(ValueError):
            analyze(self.normal, expected_clicks=40, source_claim="real",
                    capture_method="synthetic", limits=(20, 25, 30))
        with self.assertRaises(ValueError):
            analyze(self.normal, expected_clicks=40, source_claim="synthetic",
                    capture_method="isolated-stereo-line", limits=(20, 25, 30))

    def test_real_short_capture_cannot_claim_complete(self) -> None:
        report = analyze(self.normal, expected_clicks=25, source_claim="real",
                         capture_method="isolated-stereo-mics")
        self.assertEqual(report["verdict"], "INCOMPLETE")
        self.assertTrue(any("at least 30" in x for x in report["warnings"]))

    def test_missing_predetermined_limits_does_not_pass(self) -> None:
        report = analyze(self.normal, expected_clicks=40, source_claim="synthetic",
                         capture_method="synthetic")
        self.assertEqual(report["verdict"], "MEASURED_NO_PREDECLARED_LIMITS")
        self.assertIsNone(report["limits_ms"])

    def test_mono_generator_must_not_be_misread_as_stereo_capture(self) -> None:
        output = self.directory / "generated_clicks.wav"
        generate_click_track(output, clicks=3, spacing_ms=800)
        with wave.open(str(output), "rb") as wav:
            self.assertEqual(wav.getnchannels(), 1)
            self.assertEqual(wav.getsampwidth(), 2)
            self.assertEqual(wav.getframerate(), RATE)
        with self.assertRaises(ValueError):
            detect_onsets(output, threshold=0.12, refractory_ms=400)

    def test_percentile_interpolation_and_signed_matching(self) -> None:
        self.assertEqual(percentile([0, 10, 20, 30, 40], 0.95), 38)
        signed = pair_onsets([1, 2], [0.985, 2.020], 50)
        self.assertAlmostEqual(signed[0], -15, places=6)
        self.assertAlmostEqual(signed[1], 20, places=6)
        self.assertEqual(pair_onsets([1], [1.500], 50), [])

    def test_bad_threshold_and_limits_are_rejected(self) -> None:
        with self.assertRaises(ValueError):
            self.measure(self.normal, threshold=1.1)
        with self.assertRaises(ValueError):
            analyze(self.normal, expected_clicks=40, source_claim="synthetic",
                    capture_method="synthetic", limits=(20, 10, 30))


if __name__ == "__main__":
    unittest.main()
