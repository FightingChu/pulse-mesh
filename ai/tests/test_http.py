import io
import json
import math
import os
import socket
import sys
import threading
import time
import unittest
import urllib.request
import urllib.error
from http.client import HTTPConnection, HTTPResponse
from http.server import HTTPServer
from unittest.mock import patch

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
                urllib.request.urlopen(base + '/health', timeout=5).close()
            with ctx.exception as response:
                self.assertEqual(response.code, 503)
                self.assertEqual(response.headers.get('Retry-After'), '1')
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

    def test_slow_client_times_out_and_releases_worker(self):
        server = main.BoundedThreadingHTTPServer(
            ('127.0.0.1', 0), main.H, max_workers=1, request_timeout=0.1
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        address = server.server_address
        try:
            with socket.create_connection(address, timeout=2) as slow_client:
                slow_client.sendall(b'GET /health HTTP/1.1\r\nHost: 127.0.0.1\r\n')
                time.sleep(0.2)
            with urllib.request.urlopen(
                'http://127.0.0.1:%d/health' % address[1], timeout=2
            ) as response:
                self.assertEqual(response.status, 200)
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    def test_slow_overload_headers_do_not_block_accept_loop(self):
        request_entered = threading.Event()
        rejection_entered = threading.Event()
        release_worker = threading.Event()

        class SlowHandler(main.H):
            def do_GET(self):
                if self.path == '/slow':
                    request_entered.set()
                    release_worker.wait(timeout=3)
                    self._send({'status': 'ok'})
                    return
                super().do_GET()

        class ObservableServer(main.BoundedThreadingHTTPServer):
            def _reject_overloaded_request(self, request):
                rejection_entered.set()
                super()._reject_overloaded_request(request)

        server = ObservableServer(
            ('127.0.0.1', 0),
            SlowHandler,
            max_workers=1,
            request_timeout=2,
            max_rejection_workers=2,
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = 'http://127.0.0.1:%d' % server.server_port
        slow_result = []

        def request_slow():
            try:
                with urllib.request.urlopen(base + '/slow', timeout=4) as response:
                    slow_result.append(response.status)
            except Exception as exc:
                slow_result.append(exc)

        slow_thread = threading.Thread(target=request_slow)
        slow_thread.start()
        stalled_client = None
        try:
            self.assertTrue(request_entered.wait(timeout=2))
            stalled_client = socket.create_connection(server.server_address, timeout=2)
            stalled_client.sendall(b'GET /blocked HTTP/1.1\r\nHost: 127.0.0.1\r\n')
            self.assertTrue(rejection_entered.wait(timeout=2))
            with self.assertRaises(urllib.error.HTTPError) as ctx:
                urllib.request.urlopen(base + '/health', timeout=1).close()
            with ctx.exception as response:
                self.assertEqual(response.code, 503)
        finally:
            if stalled_client is not None:
                stalled_client.close()
            release_worker.set()
            slow_thread.join(timeout=5)
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
        self.assertEqual(slow_result, [200])

    def test_overloaded_expect_continue_gets_final_response(self):
        entered = threading.Event()
        release = threading.Event()

        class SlowHandler(main.H):
            def do_GET(self):
                entered.set()
                release.wait(timeout=3)
                self._send({'status': 'ok'})

        server = main.BoundedThreadingHTTPServer(
            ('127.0.0.1', 0), SlowHandler, max_workers=1, request_timeout=2
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        address = server.server_address
        slow_thread = threading.Thread(
            target=lambda: urllib.request.urlopen(
                'http://127.0.0.1:%d/slow' % address[1], timeout=4
            ).close()
        )
        slow_thread.start()
        try:
            self.assertTrue(entered.wait(timeout=2))
            with socket.create_connection(address, timeout=2) as client:
                client.sendall(
                    b'POST /spectrum HTTP/1.1\r\n'
                    b'Host: 127.0.0.1\r\n'
                    b'Content-Length: 1048576\r\n'
                    b'Expect: 100-continue\r\n'
                    b'Connection: close\r\n\r\n'
                )
                response = client.recv(1024)
            self.assertIn(b'503 Service Unavailable', response)
            self.assertNotIn(b'100 Continue', response)
        finally:
            release.set()
            slow_thread.join(timeout=5)
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)

    def test_server_close_waits_for_rejection_workers(self):
        slow_entered = threading.Event()
        rejection_entered = threading.Event()
        release_rejection = threading.Event()

        class SlowHandler(main.H):
            def do_GET(self):
                slow_entered.set()
                threading.Event().wait(timeout=2)
                self._send({'status': 'ok'})

        class ObservableServer(main.BoundedThreadingHTTPServer):
            def _reject_overloaded_request(self, request):
                rejection_entered.set()
                release_rejection.wait(timeout=3)
                super()._reject_overloaded_request(request)

        server = ObservableServer(
            ('127.0.0.1', 0),
            SlowHandler,
            max_workers=1,
            max_rejection_workers=1,
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        slow_result = []

        def request_slow():
            try:
                with urllib.request.urlopen(
                    'http://127.0.0.1:%d/slow' % server.server_port, timeout=4
                ) as response:
                    slow_result.append(response.status)
            except Exception as exc:
                slow_result.append(exc)

        slow_thread = threading.Thread(target=request_slow)
        slow_thread.start()
        stalled_client = None
        close_result = []
        close_thread = None
        try:
            self.assertTrue(slow_entered.wait(timeout=2))
            stalled_client = socket.create_connection(server.server_address, timeout=2)
            stalled_client.sendall(b'GET /blocked HTTP/1.1\r\nHost: 127.0.0.1\r\n')
            self.assertTrue(rejection_entered.wait(timeout=2))
            server.shutdown()
            close_thread = threading.Thread(target=lambda: close_result.append(server.server_close()))
            close_thread.start()
            time.sleep(0.05)
            self.assertTrue(close_thread.is_alive(), 'server_close returned before rejection worker exited')
        finally:
            release_rejection.set()
            if stalled_client is not None:
                stalled_client.close()
            slow_thread.join(timeout=5)
            if close_thread is not None:
                close_thread.join(timeout=5)
            else:
                server.shutdown()
            thread.join(timeout=5)
        self.assertEqual(slow_result, [200])
        self.assertEqual(close_result, [None])

    def test_health(self):
        with urllib.request.urlopen(self.base + "/health", timeout=5) as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(json.load(resp), {"status": "ok"})

    def test_get_body_is_not_executed_as_another_request(self):
        embedded_request = (
            b"POST /assess HTTP/1.1\r\nHost: localhost\r\n"
            b"Content-Length: 2\r\nConnection: close\r\n\r\n{}"
        )
        for path in ("/health", "/health/live", "/unknown"):
            for framing in (
                ("Content-Length: %d\r\n" % len(embedded_request)).encode(),
                b"Transfer-Encoding: chunked\r\n",
                b"Content-Length: 0\r\nContent-Length: 100\r\n",
                b"Content-Length: +0\r\n",
            ):
                with self.subTest(path=path, framing=framing):
                    with patch.object(main, "assess", wraps=main.assess) as analyze:
                        with socket.create_connection(self.server.server_address, timeout=5) as client:
                            client.sendall(
                                ("GET %s HTTP/1.1\r\nHost: localhost\r\n" % path).encode()
                                + framing + b"\r\n" + embedded_request
                            )
                            chunks = []
                            while chunk := client.recv(4096):
                                chunks.append(chunk)
                        response = b"".join(chunks)
                        self.assertTrue(response.startswith(b"HTTP/1.1 400 "), response)
                        self.assertEqual(response.count(b"HTTP/1.1 "), 1, response)
                        headers, body = response.split(b"\r\n\r\n", 1)
                        self.assertIn(b"Content-Type: application/json", headers)
                        self.assertIn("error", json.loads(body))
                        analyze.assert_not_called()

    def test_bodyless_get_requests_preserve_keep_alive(self):
        connection = HTTPConnection(*self.server.server_address, timeout=5)
        self.addCleanup(connection.close)
        connection.connect()
        original_socket = connection.sock
        for headers in ({}, {"Content-Length": "0"}, {"Content-Length": " 00\t"}):
            for path, status in (("/health", 200), ("/health/live", 200), ("/unknown", 404)):
                with self.subTest(headers=headers, path=path):
                    connection.request("GET", path, headers=headers)
                    with connection.getresponse() as response:
                        self.assertEqual(response.status, status)
                        self.assertFalse(response.will_close)
                        payload = json.load(response)
                        self.assertEqual(payload, {"status": "ok"} if status == 200 else {"error": "not found"})
                    self.assertIs(connection.sock, original_socket)

    def test_malformed_get_targets_return_json_error_and_close(self):
        embedded_request = (
            b"POST /assess HTTP/1.1\r\nHost: localhost\r\n"
            b"Content-Length: 2\r\nConnection: close\r\n\r\n{}"
        )
        for target in (b"http://[/health", b"http://[not-ip]/health", b"http://[::1/health"):
            with self.subTest(target=target):
                output = io.StringIO()
                with patch.object(main.sys, "stderr", output):
                    with patch.object(main, "assess", wraps=main.assess) as analyze:
                        with socket.create_connection(self.server.server_address, timeout=5) as client:
                            client.sendall(b"GET " + target + b" HTTP/1.1\r\nHost: localhost\r\n\r\n" + embedded_request)
                            chunks = []
                            while chunk := client.recv(4096):
                                chunks.append(chunk)
                        response = b"".join(chunks)
                        self.assertTrue(response.startswith(b"HTTP/1.1 400 "), response)
                        self.assertEqual(response.count(b"HTTP/1.1 "), 1, response)
                        headers, body = response.split(b"\r\n\r\n", 1)
                        self.assertIn(b"Content-Type: application/json", headers)
                        self.assertIn(("Content-Length: %d\r\n" % len(body)).encode(), headers + b"\r\n")
                        self.assertIn("error", json.loads(body))
                        analyze.assert_not_called()
                self.assertNotIn("Traceback", output.getvalue())
        with urllib.request.urlopen(self.base + "/health", timeout=5) as response:
            self.assertEqual(response.status, 200)

    def test_get_routing_preserves_absolute_targets_and_queries(self):
        connection = HTTPConnection(*self.server.server_address, timeout=5)
        self.addCleanup(connection.close)
        connection.connect()
        original_socket = connection.sock
        for target, status in (
            ("/health?probe=1", 200),
            ("http://localhost/health/live?probe=1", 200),
            ("http://[::1]/health", 200),
            ("http://localhost/unknown?probe=1", 404),
        ):
            with self.subTest(target=target):
                connection.request("GET", target)
                with connection.getresponse() as response:
                    self.assertEqual(response.status, status)
                    self.assertFalse(response.will_close)
                    self.assertEqual(json.load(response), {"status": "ok"} if status == 200 else {"error": "not found"})
                self.assertIs(connection.sock, original_socket)

    def test_request_control_characters_are_escaped_in_access_log(self):
        output = io.StringIO()
        with patch.dict(os.environ, {"PULSE_ACCESS_LOG": "1"}):
            with patch.object(main.sys, "stderr", output):
                with socket.create_connection(self.server.server_address, timeout=5) as client:
                    client.sendall(b"GET /sensor\x1b[2J HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
                    with HTTPResponse(client) as response:
                        response.begin()
                        self.assertEqual(response.status, 404)
                        self.assertEqual(json.load(response), {"error": "not found"})
        text = output.getvalue()
        self.assertNotIn("\x1b", text)
        self.assertIn(r"/sensor\x1b[2J", text)
        self.assertEqual(text.count("\n"), 1)

    def test_unknown_get_route_is_not_found(self):
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(self.base + "/unknown", timeout=5).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 404)

    def test_unknown_post_route_is_not_found(self):
        req = urllib.request.Request(
            self.base + "/unknown",
            data=b"{}",
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=5).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 404)

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

    def test_post_numeric_overflow_returns_json_error(self):
        maximum = float.fromhex("0x1.fffffffffffffp+1023")
        cases = (
            ("/assess", [0.0, 0.0, 0.0, 1e308]),
            ("/spectrum", [maximum] * 512 + [-maximum] * 512),
        )
        for index, (path, values) in enumerate(cases):
            with self.subTest(path=path, case=index):
                req = urllib.request.Request(
                    self.base + path,
                    data=json.dumps({"device_id": "motor-01", "values": values}, allow_nan=False).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with self.assertRaises(urllib.error.HTTPError) as ctx:
                    urllib.request.urlopen(req, timeout=5).close()
                with ctx.exception as response:
                    self.assertEqual(response.code, 400)
                    self.assertEqual(response.headers.get_content_type(), "application/json")
                    raw = response.read()
                    self.assertEqual(int(response.headers["Content-Length"]), len(raw))
                    payload = json.loads(raw)
                    self.assertIn("numeric range", payload["error"])
                    self.assertNotIn("level", payload)
                    self.assertNotIn("fault", payload)
                    json.dumps(payload, allow_nan=False)
        with urllib.request.urlopen(self.base + "/health", timeout=5) as response:
            self.assertEqual(response.status, 200)

    def test_post_assess_preserves_large_representable_values(self):
        for index, values in enumerate(([1e308] * 6, [1e200, -1e200] * 3, [1e308])):
            with self.subTest(case=index):
                request = urllib.request.Request(
                    self.base + "/assess",
                    data=json.dumps({"device_id": "motor-01", "values": values}).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    payload = json.load(response, parse_constant=lambda value: self.fail(value))
                self.assertTrue(math.isfinite(payload["rms"]))
                self.assertAlmostEqual(payload["rms"] / max(abs(value) for value in values), 1.0)
                self.assertEqual(payload["level"], "normal")

    def test_post_spectrum_preserves_large_representable_amplitudes(self):
        reference_values = [math.sin(2 * math.pi * 120 * index / 1000) for index in range(1024)]
        values = [1e200 * value for value in reference_values]
        expected = main.diagnose("motor-01", reference_values)["peak_amplitude"]
        request = urllib.request.Request(
            self.base + "/spectrum",
            data=json.dumps({"device_id": "motor-01", "values": values}).encode(),
            headers={"Content-Type": "application/json"},
        )
        with urllib.request.urlopen(request, timeout=5) as response:
            self.assertEqual(response.status, 200)
            payload = json.load(response, parse_constant=lambda value: self.fail(value))
        self.assertTrue(math.isfinite(payload["peak_amplitude"]))
        self.assertAlmostEqual(payload["peak_amplitude"] / 1e200, expected, delta=0.01)
        self.assertEqual(payload["fault"], "bearing_wear")

    def test_post_spectrum_preserves_vibration_with_large_dc_offsets(self):
        for offset in (-1e16, 1e16):
            with self.subTest(offset=offset):
                values = [offset + value for value in (2.0, 0.0, -2.0, 0.0)] * 256
                request = urllib.request.Request(
                    self.base + "/spectrum",
                    data=json.dumps({"device_id": "motor-01", "values": values}, allow_nan=False).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    payload = json.load(response, parse_constant=lambda value: self.fail(value))
                self.assertEqual(payload["device_id"], "motor-01")
                self.assertEqual(payload["dominant_hz"], 250.0)
                self.assertEqual(payload["peak_amplitude"], 2.0)
                self.assertEqual(payload["low_band"], 0.0)
                self.assertEqual(payload["mid_band"], 0.0)
                self.assertEqual(payload["high_band"], 1.0)
                self.assertEqual(payload["fault"], "bearing_wear")

    def test_post_assess_preserves_variance_with_extreme_spikes(self):
        for spike in (1e300, -1e308):
            with self.subTest(spike=spike):
                values = [1e100, -1e100, 1e100, -1e100, spike]
                request = urllib.request.Request(
                    self.base + "/assess",
                    data=json.dumps({"device_id": "motor-01", "values": values}, allow_nan=False).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    payload = json.load(response, parse_constant=lambda value: self.fail(value))
                self.assertEqual(payload["device_id"], "motor-01")
                self.assertEqual(payload["last"], spike)
                self.assertEqual(payload["level"], "critical")
                self.assertAlmostEqual(payload["z"] / (spike / 1e100), 1.0, places=12)
                self.assertTrue(math.isfinite(payload["rms"]))

    def test_post_assess_preserves_levels_with_large_baselines(self):
        for offset in (-1e16, 1e16):
            for value, level in ((4.0, "normal"), (6.0, "warning"), (10.0, "critical")):
                with self.subTest(offset=offset, value=value):
                    values = [offset + item for item in (-2.0, 0.0, 2.0, 0.0, value)]
                    request = urllib.request.Request(
                        self.base + "/assess",
                        data=json.dumps({"device_id": "motor-01", "values": values}, allow_nan=False).encode(),
                        headers={"Content-Type": "application/json"},
                    )
                    with urllib.request.urlopen(request, timeout=5) as response:
                        self.assertEqual(response.status, 200)
                        payload = json.load(response, parse_constant=lambda item: self.fail(item))
                    self.assertEqual(payload["z"], round(value / math.sqrt(2.0), 3))
                    self.assertEqual(payload["level"], level)
                    self.assertEqual(payload["last"], values[-1])

    def test_post_rejects_invalid_sample_types(self):
        invalid_values = [None, False, 0, "", {}, {"0": 0}]
        invalid_values.extend(
            [invalid] + [0.0] * 1023
            for invalid in (True, False, None, "0", [], {}, 10**400)
        )
        for path in ("/assess", "/spectrum"):
            for index, values in enumerate(invalid_values):
                with self.subTest(path=path, case=index):
                    req = urllib.request.Request(
                        self.base + path,
                        data=json.dumps({"device_id": "motor-01", "values": values}).encode(),
                        headers={"Content-Type": "application/json"},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5).close()
                    with ctx.exception as response:
                        self.assertEqual(response.code, 400)
                        self.assertIn("error", json.load(response))

    def test_post_spectrum_rejects_non_numeric_frequencies(self):
        for field in ("sample_rate_hz", "rotation_hz"):
            for invalid in (True, False, "1000", None, [], {}, 10**400):
                with self.subTest(field=field, invalid=repr(invalid)):
                    req = urllib.request.Request(
                        self.base + "/spectrum",
                        data=json.dumps({
                            "device_id": "motor-01", "values": [0.0] * 1024, field: invalid,
                        }).encode(),
                        headers={"Content-Type": "application/json"},
                    )
                    with self.assertRaises(urllib.error.HTTPError) as ctx:
                        urllib.request.urlopen(req, timeout=5).close()
                    with ctx.exception as response:
                        self.assertEqual(response.code, 400)
                        self.assertIn("error", json.load(response))

    def test_post_spectrum_preserves_numeric_zeros_and_defaults(self):
        for frequencies in ({}, {"sample_rate_hz": 1000, "rotation_hz": 0}):
            with self.subTest(frequencies=frequencies):
                req = urllib.request.Request(
                    self.base + "/spectrum",
                    data=json.dumps({
                        "device_id": "motor-01", "event_id": "zero-waveform",
                        "values": [0, 0.0] * 512, **frequencies,
                    }).encode(),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(req, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    payload = json.load(response)
                self.assertEqual(payload["fault"], "normal")
                self.assertEqual(payload["event_id"], "zero-waveform")
                for feature in ("dominant_hz", "peak_amplitude", "low_band", "mid_band", "high_band"):
                    self.assertEqual(payload[feature], 0.0)

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
            urllib.request.urlopen(req, timeout=10).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 400)

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
            urllib.request.urlopen(req, timeout=10).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 400)

    def test_post_spectrum_rejects_low_sample_rate(self):
        body = json.dumps({
            "device_id": "motor-01",
            "values": [0.0] * 1024,
            "sample_rate_hz": 200,
            "rotation_hz": 25,
        }).encode()
        req = urllib.request.Request(
            self.base + "/spectrum",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 400)

    def test_post_spectrum_requires_string_identity(self):
        for patch in ({"device_id": ""}, {"event_id": []}):
            payload = {
                "device_id": "motor-01",
                "event_id": "boot-1-spectrum-0",
                "values": [0.0] * 1024,
            }
            payload.update(patch)
            req = urllib.request.Request(
                self.base + "/spectrum",
                data=json.dumps(payload).encode(),
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            with self.assertRaises(urllib.error.HTTPError) as ctx:
                urllib.request.urlopen(req, timeout=10).close()
            with ctx.exception as response:
                self.assertEqual(response.code, 400)

    def test_post_rejects_multiple_json_values(self):
        req = urllib.request.Request(
            self.base + "/assess",
            data=b'{"values":[]} {}',
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            urllib.request.urlopen(req, timeout=10).close()
        with ctx.exception as response:
            self.assertEqual(response.code, 400)

    def test_post_rejects_deeply_nested_json_before_analysis(self):
        for path, operation in (("/assess", "assess"), ("/spectrum", "diagnose")):
            for depth in (2000, 3000):
                with self.subTest(path=path, depth=depth):
                    body = b'{"ignored":' + b"[" * depth + b"0" + b"]" * depth + b',"values":[]}'
                    request = urllib.request.Request(
                        self.base + path,
                        data=body,
                        headers={"Content-Type": "application/json"},
                    )
                    with patch.object(main, operation, wraps=getattr(main, operation)) as analyze:
                        with self.assertRaises(urllib.error.HTTPError) as ctx:
                            urllib.request.urlopen(request, timeout=5).close()
                        with ctx.exception as response:
                            self.assertEqual(response.code, 400)
                            self.assertEqual(response.headers.get_content_type(), "application/json")
                            error = json.load(response)["error"].lower()
                            self.assertTrue("depth" in error or "nest" in error, error)
                        analyze.assert_not_called()

    def test_post_rejects_non_json_whitespace_before_analysis(self):
        body = json.dumps({"device_id": "motor-01", "values": [0.0] * 1024})
        for path, operation in (("/assess", "assess"), ("/spectrum", "diagnose")):
            for whitespace in ("\v", "\f", "\u00a0", "\u2003", "\u2028"):
                for text in (whitespace + body, body + whitespace):
                    with self.subTest(path=path, prefix=repr(text[:1]), suffix=repr(text[-1:])):
                        request = urllib.request.Request(
                            self.base + path,
                            data=text.encode("utf-8"),
                            headers={"Content-Type": "application/json"},
                        )
                        with patch.object(main, operation, wraps=getattr(main, operation)) as analyze:
                            with self.assertRaises(urllib.error.HTTPError) as ctx:
                                urllib.request.urlopen(request, timeout=5).close()
                            with ctx.exception as response:
                                self.assertEqual(response.code, 400)
                                self.assertEqual(response.headers.get_content_type(), "application/json")
                                self.assertIn("error", json.load(response))
                            analyze.assert_not_called()

    def test_post_preserves_json_whitespace_and_unicode_strings(self):
        device = "电机\u00a0\u2003\u2028east"
        body = json.dumps({"device_id": device, "values": [0.0] * 1024}, ensure_ascii=False)
        for path, field in (("/assess", "level"), ("/spectrum", "fault")):
            with self.subTest(path=path):
                request = urllib.request.Request(
                    self.base + path,
                    data=(" \t\r\n" + body + "\n\r\t ").encode("utf-8"),
                    headers={"Content-Type": "application/json"},
                )
                with urllib.request.urlopen(request, timeout=5) as response:
                    self.assertEqual(response.status, 200)
                    payload = json.load(response)
                self.assertEqual(payload["device_id"], device)
                self.assertEqual(payload[field], "normal")

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

    def test_post_rejects_truncated_body_before_analysis(self):
        for path, operation in (("/assess", "assess"), ("/spectrum", "diagnose")):
            complete_body = json.dumps({"device_id": "motor-01", "values": [0.0] * 1024}).encode()
            for body in (b"", b'{"values":', complete_body):
                with self.subTest(path=path, received=len(body)):
                    with patch.object(main, operation, wraps=getattr(main, operation)) as analyze:
                        with socket.create_connection(self.server.server_address, timeout=5) as client:
                            client.sendall(
                                ("POST %s HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\n\r\n" % (path, len(body) + 1)).encode()
                                + body
                            )
                            client.shutdown(socket.SHUT_WR)
                            with HTTPResponse(client) as response:
                                response.begin()
                                self.assertEqual(response.status, 400)
                                self.assertEqual(response.headers.get_content_type(), "application/json")
                                raw = response.read()
                                self.assertEqual(int(response.headers["Content-Length"]), len(raw))
                                self.assertEqual(json.loads(raw), {"error": "incomplete request body"})
                            self.assertEqual(client.recv(1), b"")
                        analyze.assert_not_called()

    def test_complete_request_bodies_preserve_keep_alive(self):
        connection = HTTPConnection(*self.server.server_address, timeout=5)
        self.addCleanup(connection.close)
        connection.connect()
        original_socket = connection.sock
        for path, body, field in (
            ("/assess", b"", "level"),
            ("/assess", b'{"values":[0,0.0]}', "level"),
            ("/spectrum", json.dumps({"device_id": "motor-01", "values": [0.0] * 1024}).encode(), "fault"),
        ):
            with self.subTest(path=path, body_length=len(body)):
                connection.request("POST", path, body=body, headers={"Content-Type": "application/json"})
                with connection.getresponse() as response:
                    self.assertEqual(response.status, 200)
                    self.assertFalse(response.will_close)
                    self.assertEqual(json.load(response)[field], "normal")
                self.assertIs(connection.sock, original_socket)
        connection.request("GET", "/health")
        with connection.getresponse() as response:
            self.assertEqual(response.status, 200)
            self.assertEqual(json.load(response), {"status": "ok"})
        self.assertIs(connection.sock, original_socket)

    def test_post_rejects_invalid_content_length_headers(self):
        cases = [
            (b"", b"411"),
            (b"Content-Length: invalid\r\n", b"400"),
            (b"Content-Length: 2\r\nContent-Length: 2\r\n", b"411"),
            (b"Transfer-Encoding: chunked\r\n", b"400"),
        ]
        for headers, expected_status in cases:
            with self.subTest(headers=headers):
                with socket.create_connection(self.server.server_address, timeout=5) as client:
                    request = (
                        b"POST /assess HTTP/1.1\r\n"
                        b"Host: 127.0.0.1\r\n"
                        + headers
                        + b"Connection: close\r\n\r\n"
                    )
                    client.sendall(request)
                    response = client.recv(1024)
                self.assertIn(expected_status, response)

    def test_post_rejects_non_decimal_content_length(self):
        for length in (b"+2", b"0_2", b"-0", b"-2", b"", b"2,2", b"\xa02", b"2\xa0", b"\x0b2", b"2\x0b"):
            with self.subTest(length=length):
                with patch.object(main, "assess", wraps=main.assess) as analyze:
                    with socket.create_connection(self.server.server_address, timeout=5) as client:
                        client.sendall(
                            b"POST /assess HTTP/1.1\r\nHost: localhost\r\nContent-Length: "
                            + length + b"\r\nConnection: close\r\n\r\n{}"
                        )
                        with HTTPResponse(client) as response:
                            response.begin()
                            self.assertEqual(response.status, 400)
                            self.assertEqual(json.load(response), {"error": "invalid content-length"})
                    analyze.assert_not_called()

    def test_post_accepts_decimal_content_length_with_http_whitespace(self):
        for length in (b"2", b"02", b" \t2\t "):
            with self.subTest(length=length):
                with socket.create_connection(self.server.server_address, timeout=5) as client:
                    client.sendall(
                        b"POST /assess HTTP/1.1\r\nHost: localhost\r\nContent-Length: "
                        + length + b"\r\nConnection: close\r\n\r\n{}"
                    )
                    with HTTPResponse(client) as response:
                        response.begin()
                        self.assertEqual(response.status, 200)
                        self.assertEqual(json.load(response)["level"], "normal")


if __name__ == "__main__":
    unittest.main()
