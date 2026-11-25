import os
import socket
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "app"))

import main


class ServerLimitTest(unittest.TestCase):
    def test_invalid_request_timeouts_fail_before_binding(self):
        for value in (None, True, False, 0, -1, float("nan"), float("inf"), float("-inf"), "5", 10**400, main.threading.TIMEOUT_MAX):
            with self.subTest(value=value):
                with patch.object(main.ThreadingHTTPServer, "__init__", return_value=None) as initialize:
                    with self.assertRaises(ValueError):
                        main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, request_timeout=value)
                    initialize.assert_not_called()

    def test_valid_request_timeouts_are_applied_to_connections(self):
        for timeout in (0.1, main.REQUEST_TIMEOUT_SECONDS, 60):
            with self.subTest(timeout=timeout):
                server = main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, request_timeout=timeout)
                try:
                    with socket.create_connection(server.server_address, timeout=5):
                        connection, address = server.get_request()
                        with connection:
                            self.assertEqual(connection.gettimeout(), timeout)
                            self.assertEqual(address[0], "127.0.0.1")
                finally:
                    server.server_close()

    def test_request_timeout_respects_platform_socket_limit(self):
        timeout = main.threading.TIMEOUT_MAX - 1
        with socket.socket() as probe:
            try:
                probe.settimeout(timeout)
            except (ValueError, OverflowError):
                with patch.object(main.ThreadingHTTPServer, "__init__", return_value=None) as initialize:
                    with self.assertRaisesRegex(ValueError, "platform socket limit"):
                        main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, request_timeout=timeout)
                    initialize.assert_not_called()
            else:
                server = main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, request_timeout=timeout)
                try:
                    with socket.create_connection(server.server_address, timeout=5):
                        connection, address = server.get_request()
                        with connection:
                            self.assertEqual(connection.gettimeout(), timeout)
                            self.assertEqual(address[0], "127.0.0.1")
                finally:
                    server.server_close()

    def test_invalid_worker_limits_fail_before_binding(self):
        for field, maximum in (("max_workers", 64), ("max_rejection_workers", 16)):
            for value in (0, -1, maximum + 1, 1.5, 1.0, float("nan"), float("inf"), True, False, "2", None):
                with self.subTest(field=field, value=value):
                    with patch.object(main.ThreadingHTTPServer, "__init__", return_value=None) as initialize:
                        with self.assertRaises(ValueError):
                            main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, **{field: value})
                        initialize.assert_not_called()

    def test_integer_worker_limits_are_exact_and_reusable(self):
        for field, attribute, maximum in (
            ("max_workers", "_worker_slots", 64),
            ("max_rejection_workers", "_rejection_slots", 16),
        ):
            for limit in (1, maximum):
                with self.subTest(field=field, limit=limit):
                    server = main.BoundedThreadingHTTPServer(("127.0.0.1", 0), main.H, **{field: limit})
                    self.addCleanup(server.server_close)
                    slots = getattr(server, attribute)
                    for attempt in range(2):
                        for index in range(limit):
                            self.assertTrue(slots.acquire(blocking=False), (attempt, index))
                        self.assertFalse(slots.acquire(blocking=False))
                        for index in range(limit):
                            slots.release()
                    server.server_close()


if __name__ == "__main__":
    unittest.main()
