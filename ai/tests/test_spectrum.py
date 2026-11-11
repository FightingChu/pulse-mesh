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

    def test_rejects_short_series(self):
        with self.assertRaises(ValueError):
            spectrum.analyze([1.0, 2.0, 3.0], RATE)

    def test_rejects_bad_sample_rate(self):
        with self.assertRaises(ValueError):
            spectrum.analyze(tone(25.0), 0)


if __name__ == "__main__":
    unittest.main()
