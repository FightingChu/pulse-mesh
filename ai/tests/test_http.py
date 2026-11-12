import json
import math
import os
import socket
import sys
import threading
import unittest
import urllib.request
import urllib.error
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

    def test_bounded_server_rejects_excess_connections(self):
        entered = threading.Event()
        release = threading.Event()

        class SlowHandler(main.H):
            def do_GET(self):
                if self.path == '/slow':
                    entered.set()
                    release.wait(timeout=3)
                    self._send({'status': 'ok'})
                    return
                super().do_GET()

        server = main.BoundedThreadingHTTPServer(('127.0.0.1', 0), SlowHandler, max_workers=1)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = 'http://127.0.0.1:%d' % server.server_port
        slow_result = []

        def request_slow():
            try:
                with urllib.request.urlopen(base + '/slow', timeout=5) as response:
                    slow_result.append(response.status)
            except Exception as exc:
                slow_result.append(exc)

        slow_thread = threading.Thread(target=request_slow)
        slow_thread.start()
        self.assertTrue(entered.wait(timeout=2), 'slow request did not occupy the worker')
        try:
            with self.assertRaises(urllib.error.HTTPError) as ctx:
                urllib.request.urlopen(base + '/health', timeout=5)
            self.assertEqual(ctx.exception.code, 503)
            self.assertEqual(ctx.exception.headers.get('Retry-After'), '1')
        finally:
            release.set()
            slow_thread.join(timeout=5)
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
        self.assertEqual(slow_result, [200])

    def test_bounded_server_validates_worker_limit(self):
        with self.assertRaises(ValueError):
            main.BoundedThreadingHTTPServer(('127.0.0.1', 0), main.H, max_workers=0)

    def test_health(self):
        with urllib.request.urlopen(self.base + "/health", timeout=5) as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(json.load(resp), {"status": "ok"})

    def test_unknown_get_route_is_not_found(self):
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(self.base + "/unknown", timeout=5)
        self.assertEqual(ctx.exception.code, 404)

    def test_unknown_post_route_is_not_found(self):
        req = urllib.request.Request(
            self.base + "/unknown",
            data=b"{}",
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=5)
        self.assertEqual(ctx.exception.code, 404)

    def test_post_assess_critical(self):
        body = json.dumps({
            "event_id": "boot-1-spectrum-0",
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

    def test_post_spectrum_bearing(self):
        values = [
            0.6 * math.sin(2.0 * math.pi * 120.0 * i / 1000.0) for i in range(1024)
        ]
        body = json.dumps({
            "event_id": "boot-1-spectrum-0",
            "device_id": "motor-01",
            "values": values,
            "sample_rate_hz": 1000.0,
            "rotation_hz": 25.0,
        }).encode()
        req = urllib.request.Request(
            self.base + "/spectrum",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            payload = json.load(resp)
        self.assertEqual(payload["device_id"], "motor-01")
        self.assertEqual(payload["event_id"], "boot-1-spectrum-0")
        self.assertEqual(payload["fault"], "bearing_wear")
        self.assertAlmostEqual(payload["dominant_hz"], 120.0, delta=2.0)

    def test_post_spectrum_rejects_bad_length(self):
        body = json.dumps({"values": [1.0, 2.0, 3.0]}).encode()
        req = urllib.request.Request(
            self.base + "/spectrum",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10)
        self.assertEqual(ctx.exception.code, 400)

    def test_post_spectrum_rejects_non_finite_values(self):
        body = json.dumps({
            "device_id": "motor-01",
            "values": [float("nan")] + [0.0] * 1023,
        }).encode()
        req = urllib.request.Request(
            self.base + "/spectrum",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10)
        self.assertEqual(ctx.exception.code, 400)

    def test_post_rejects_multiple_json_values(self):
        req = urllib.request.Request(
            self.base + "/assess",
            data=b'{"values":[]} {}',
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10)
        self.assertEqual(ctx.exception.code, 400)

    def test_post_rejects_oversized_body(self):
        address = self.server.server_address
        with socket.create_connection(address, timeout=5) as client:
            client.sendall(
                b"POST /assess HTTP/1.1\r\n"
                b"Host: 127.0.0.1\r\n"
                b"Content-Length: 131073\r\n"
                b"Connection: close\r\n\r\n"
            )
            response = client.recv(1024)
        self.assertIn(b"413", response)


if __name__ == "__main__":
    unittest.main()
