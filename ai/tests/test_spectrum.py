import math
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

import spectrum

RATE = 1000.0
N = 1024


def tone(hz, amplitude=1.0, n=N, rate=RATE):
    return [amplitude * math.sin(2.0 * math.pi * hz * i / rate) for i in range(n)]


class SpectrumTest(unittest.TestCase):
    def test_bin_centered_peak_amplitudes(self):
        for size in (8, 64, 1024):
            for cycle in ((3.0, -3.0), (3.0, 0.0, -3.0, 0.0)):
                with self.subTest(size=size, period=len(cycle)):
                    result = spectrum.analyze(cycle * (size // len(cycle)), RATE)
                    self.assertEqual(result["dominant_hz"], RATE / len(cycle))
                    self.assertEqual(result["peak_amplitude"], 3.0)

    def test_fft_rejects_bad_length(self):
        with self.assertRaises(ValueError):
            spectrum.fft([1.0, 2.0, 3.0])

    def test_tone_25hz_is_imbalance(self):
        result = spectrum.diagnose(tone(25.0), RATE, 25.0)
        self.assertAlmostEqual(result["dominant_hz"], 25.0, delta=2.0)
        self.assertAlmostEqual(result["peak_amplitude"], 1.0, delta=0.1)
        self.assertEqual(result["fault"], "imbalance")

    def test_tone_50hz_is_misalignment(self):
        result = spectrum.diagnose(tone(50.0), RATE, 25.0)
        self.assertAlmostEqual(result["dominant_hz"], 50.0, delta=2.0)
        self.assertEqual(result["fault"], "misalignment")

    def test_tone_120hz_is_bearing_wear(self):
        result = spectrum.diagnose(tone(120.0, 0.6), RATE, 25.0)
        self.assertAlmostEqual(result["dominant_hz"], 120.0, delta=2.0)
        self.assertGreater(result["high_band"], 0.25)
        self.assertEqual(result["fault"], "bearing_wear")

    def test_flat_signal_is_normal(self):
        result = spectrum.diagnose([0.0] * N, RATE, 25.0)
        self.assertEqual(result["fault"], "normal")

    def test_fault_uses_band_energy_before_reporting_precision(self):
        for hz, band, threshold, fault in (
            (128, "high_band", 0.25, "bearing_wear"),
            (4, "low_band", 0.5, "looseness"),
        ):
            for delta, expected in ((-0.00003, "normal"), (0.00003, fault)):
                with self.subTest(band=band, delta=delta):
                    fraction = threshold + delta
                    amplitude = math.sqrt(fraction / (1 - fraction))
                    values = [base + component for base, component in zip(
                        tone(32, rate=N), tone(hz, amplitude, rate=N)
                    )]
                    result = spectrum.diagnose(values, N, 0)
                    self.assertEqual(result[band], threshold)
                    self.assertEqual(result["fault"], expected)

    def test_fault_uses_fft_bin_frequency_before_reporting_precision(self):
        values = tone(25 * RATE / N)
        for rotation, expected in ((30.515, "imbalance"), (20.344, "normal")):
            with self.subTest(rotation=rotation):
                result = spectrum.diagnose(values, RATE, rotation)
                self.assertEqual(result["dominant_hz"], 24.41)
                self.assertEqual(result["fault"], expected)
                self.assertEqual({key: value for key, value in result.items() if key != "fault"},
                                 spectrum.analyze(values, RATE))

    def test_rejects_short_series(self):
        with self.assertRaises(ValueError):
            spectrum.analyze([1.0, 2.0, 3.0], RATE)

    def test_rejects_bad_sample_rate(self):
        with self.assertRaises(ValueError):
            spectrum.analyze(tone(25.0), 0)

    def test_rejects_numeric_overflow_before_classification(self):
        for name, values in (
            ("mean", [1e308] * N),
            ("power", tone(120.0, 1e200)),
            ("energy_sum", [2.5e151, -2.5e151] * (N // 2)),
        ):
            with self.subTest(stage=name):
                with self.assertRaisesRegex(ValueError, "numeric range"):
                    spectrum.diagnose(values, RATE, 25.0)

    def test_large_representable_tone_preserves_features(self):
        reference = spectrum.diagnose(tone(120.0), RATE, 25.0)
        result = spectrum.diagnose(tone(120.0, 1e100), RATE, 25.0)
        self.assertEqual(result["fault"], reference["fault"])
        for feature in ("dominant_hz", "low_band", "mid_band", "high_band"):
            self.assertEqual(result[feature], reference[feature])
        self.assertAlmostEqual(result["peak_amplitude"] / 1e100, reference["peak_amplitude"], places=4)


if __name__ == "__main__":
    unittest.main()
