import os
import math
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

    def test_rms_preserves_large_finite_measurements(self):
        for value in (1e154, 1e200, 1e308, float.fromhex("0x1.fffffffffffffp+1023")):
            with self.subTest(value=value):
                result = main.assess("motor-01", "vibration_rms", [value] * 6)
                self.assertTrue(math.isfinite(result["rms"]))
                self.assertAlmostEqual(result["rms"] / value, 1.0)
                self.assertEqual(result["level"], "normal")

    def test_entry_points_reject_non_numeric_samples(self):
        for invalid in (True, False, None, "0", [], {}, 10**400, float("inf"), float("nan")):
            for operation in ("assess", "diagnose"):
                with self.subTest(operation=operation, invalid=repr(invalid)):
                    with self.assertRaises(ValueError):
                        if operation == "assess":
                            main.assess("motor-01", "vibration_rms", [0, invalid])
                        else:
                            main.diagnose("motor-01", [invalid] + [0.0] * 1023)

    def test_zscore_preserves_large_finite_readings(self):
        cases = (
            ([1e308] * 5, 1e308, 0.0),
            ([1e200, -1e200, 1e200], -1e200, -math.sqrt(2)),
            ([1e-200, -1e-200, 1e-200], -1e-200, -(4 / 3) * 1e-194),
        )
        for history, value, expected in cases:
            with self.subTest(history=history, value=value):
                actual = main.zscore(value, history)
                self.assertTrue(math.isfinite(actual))
                self.assertAlmostEqual(actual / expected if expected else actual, 1.0 if expected else 0.0, places=6)

    def test_diagnose_rejects_non_numeric_frequencies(self):
        for field in ("sample_rate_hz", "rotation_hz"):
            for invalid in (True, False, None, "1000", [], {}, 10**400, float("inf"), float("nan")):
                with self.subTest(field=field, invalid=repr(invalid)):
                    with self.assertRaises(ValueError):
                        main.diagnose("motor-01", [0.0] * 1024, **{field: invalid})

    def test_assess_rejects_numeric_overflow(self):
        for values in ([1e308, 1e308, 1e308, -1e308], [0.0, 0.0, 0.0, 1e308]):
            with self.subTest(values=values):
                with self.assertRaisesRegex(ValueError, "numeric range"):
                    main.assess("motor-01", "vibration_rms", values)

    def test_assess_preserves_large_representable_results(self):
        for values in ([1e100] * 6, [1e308] * 6, [1e200, -1e200] * 3, [1e-200, -1e-200] * 3):
            with self.subTest(values=values):
                result = main.assess("motor-01", "vibration_rms", values)
                self.assertTrue(math.isfinite(result["rms"]))
                self.assertEqual(result["z"], round(main.zscore(values[-1], main.baseline(values)), 3))
                self.assertEqual(result["level"], "normal")
                self.assertAlmostEqual(result["rms"] / max(abs(value) for value in values), 1)


if __name__ == "__main__":
    unittest.main()
