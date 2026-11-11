import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

import main


class AssessTest(unittest.TestCase):
    def test_normal_reading(self):
        result = main.assess("motor-01", "vibration_rms", [1.1, 1.2, 1.0, 1.3, 1.2, 1.25])
        self.assertEqual(result["level"], "normal")
        self.assertEqual(result["device_id"], "motor-01")
        self.assertAlmostEqual(result["last"], 1.25)

    def test_moderate_deviation_is_warning(self):
        result = main.assess("motor-01", "vibration_rms", [1.1, 1.2, 1.0, 1.3, 1.2, 1.5])
        self.assertEqual(result["level"], "warning")
        self.assertGreaterEqual(abs(result["z"]), main.WARN_Z)

    def test_spike_does_not_mask_itself(self):
        history = [1.1, 1.2, 1.0, 1.3, 1.2]
        result = main.assess("motor-01", "vibration_rms", history + [4.8])
        self.assertEqual(result["level"], "critical")
        self.assertAlmostEqual(result["z"], main.zscore(4.8, history), places=3)

    def test_extreme_spike_is_critical(self):
        result = main.assess("motor-01", "vibration_rms", [1.0, 1.0, 1.0, 1.0, 1.0, 12.0])
        self.assertEqual(result["level"], "critical")

    def test_short_series_is_neutral(self):
        result = main.assess("motor-01", "vibration_rms", [1.0, 2.0])
        self.assertEqual(result["z"], 0.0)
        self.assertEqual(result["level"], "normal")

    def test_empty_series(self):
        result = main.assess("motor-01", "vibration_rms", [])
        self.assertEqual(result["last"], 0.0)
        self.assertEqual(result["rms"], 0.0)

    def test_rms_value(self):
        self.assertAlmostEqual(main.rms([3.0, 4.0]), 3.5355339059327378)


if __name__ == "__main__":
    unittest.main()
