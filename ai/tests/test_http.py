import json
import os
import sys
import threading
import unittest
import urllib.request
from http.server import HTTPServer

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

import main


class HTTPTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = HTTPServer(("127.0.0.1", 0), main.H)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base = "http://127.0.0.1:%d" % cls.server.server_port

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)

    def test_health(self):
        with urllib.request.urlopen(self.base + "/health", timeout=5) as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(json.load(resp), {"status": "ok"})

    def test_post_assess_critical(self):
        body = json.dumps({
            "device_id": "motor-01",
            "metric": "vibration_rms",
            "values": [1.1, 1.2, 1.0, 1.3, 1.2, 4.8],
        }).encode()
        req = urllib.request.Request(
            self.base + "/assess",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=5) as resp:
            payload = json.load(resp)
        self.assertEqual(payload["device_id"], "motor-01")
        self.assertEqual(payload["level"], "critical")

    def test_post_assess_empty_values(self):
        req = urllib.request.Request(
            self.base + "/assess",
            data=b"{}",
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=5) as resp:
            payload = json.load(resp)
        self.assertEqual(payload["level"], "normal")
        self.assertEqual(payload["rms"], 0.0)


if __name__ == "__main__":
    unittest.main()
