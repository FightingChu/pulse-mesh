import json
import os
import subprocess
import unittest


class FirmwareOutputTest(unittest.TestCase):
    def test_each_reading_has_distinct_event_identity(self):
        executable = os.environ['PULSE_FIRMWARE_EXE']
        previous_events = set()
        for run in range(2):
            with self.subTest(run=run):
                completed = subprocess.run(
                    [executable], capture_output=True, text=True, check=True, timeout=10
                )
                records = [json.loads(line) for line in completed.stdout.splitlines()]
                samples = [record for record in records if record['metric'] == 'vibration_rms']
                spectra = [record for record in records if record['metric'] == 'spectrum']
                self.assertEqual(len(samples), 50)
                self.assertEqual(len(spectra), 1)
                event_ids = [record['event_id'] for record in records]
                self.assertEqual(len(set(event_ids)), len(records), 'readings share an event ID')
                self.assertTrue(all(event_ids))
                self.assertTrue(all(len(event_id) <= 128 for event_id in event_ids))
                self.assertFalse(previous_events.intersection(event_ids), 'restart reused event IDs')
                previous_events.update(event_ids)
                self.assertEqual(samples[42]['level'], 'critical')
                self.assertEqual(samples[42]['value'], 4.8)


if __name__ == '__main__':
    unittest.main()
