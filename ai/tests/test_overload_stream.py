import json
import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'app'))

import main


class ScriptedSocket:
    def __init__(self, chunks):
        self.chunks = list(chunks)
        self.response = bytearray()
        self.read_past_body = False
        self.closed = False

    def recv(self, size):
        if not self.chunks:
            self.read_past_body = True
            raise TimeoutError('client is waiting for the HTTP response')
        chunk = self.chunks[0][:size]
        self.chunks[0] = self.chunks[0][size:]
        if not self.chunks[0]:
            self.chunks.pop(0)
        return chunk

    def sendall(self, payload):
        self.response.extend(payload)

    def shutdown(self, direction):
        pass

    def close(self):
        self.closed = True


class OverloadStreamTest(unittest.TestCase):
    def reject(self, chunks):
        server = main.BoundedThreadingHTTPServer(
            ('127.0.0.1', 0), main.H, max_workers=1, max_rejection_workers=1
        )
        self.addCleanup(server.server_close)
        client = ScriptedSocket(chunks)
        self.assertTrue(server._rejection_slots.acquire(blocking=False))
        server._reject_overloaded_request(client)
        self.assertTrue(client.closed)
        self.assertTrue(server._rejection_slots.acquire(blocking=False))
        server._rejection_slots.release()
        return client

    def test_request_packet_boundaries_do_not_change_overload_response(self):
        body = b'{"values":[1,2,3,4]}'
        header = (
            b'POST /spectrum HTTP/1.1\r\nHost: localhost\r\n'
            + ('Content-Length: %d\r\n' % len(body)).encode()
            + b'Connection: close\r\n\r\n'
        )
        for name, chunks in {
            'headers then body': [header, body],
            'coalesced headers and body': [header + body],
            'split delimiter with partial body': [header[:-2], header[-2:] + body[:7], body[7:]],
            'large coalesced packet': [header + body + b'extra'],
        }.items():
            with self.subTest(packetization=name):
                client = self.reject(chunks)
                self.assertFalse(client.read_past_body, 'server waited for bytes beyond Content-Length')
                response_header, response_body = bytes(client.response).split(b'\r\n\r\n', 1)
                self.assertTrue(response_header.startswith(b'HTTP/1.1 503 '))
                self.assertIn(b'Retry-After: 1', response_header)
                self.assertIn(('Content-Length: %d' % len(response_body)).encode(), response_header)
                self.assertEqual(json.loads(response_body), {'error': 'AI service busy'})

    def test_body_header_text_is_not_parsed_as_http_header(self):
        body = b'\r\nExpect: 100-continue\r\nContent-Length: 999999\r\n\r\npayload'
        header = (
            b'POST /assess HTTP/1.1\r\n'
            + ('Content-Length: %d\r\n\r\n' % len(body)).encode()
        )
        client = self.reject([header + body[:8], body[8:]])
        self.assertFalse(client.read_past_body)
        self.assertEqual(client.chunks, [])
        self.assertTrue(client.response.startswith(b'HTTP/1.1 503 '))

    def test_header_limit_remains_bounded(self):
        client = self.reject([b'X' * (16 * 1024), b'\r\n\r\n'])
        self.assertFalse(client.read_past_body)
        self.assertEqual(client.response, b'')
        self.assertEqual(client.chunks, [b'\r\n\r\n'])

    def test_overload_rejects_non_decimal_content_length_without_reading_body(self):
        for length in (b'+2', b'0_2', b'-0', b'-2', b'', b'2,2', b'\xa02', b'2\xa0', b'\x0b2', b'2\x0b'):
            with self.subTest(length=length):
                header = b'POST /assess HTTP/1.1\r\nContent-Length: ' + length + b'\r\n\r\n'
                client = self.reject([header, b'{}'])
                self.assertFalse(client.read_past_body)
                self.assertEqual(client.response, b'')
                self.assertEqual(client.chunks, [b'{}'])

    def test_overload_accepts_decimal_content_length_with_http_whitespace(self):
        for length in (b'2', b'02', b' \t2\t '):
            with self.subTest(length=length):
                header = b'POST /assess HTTP/1.1\r\nContent-Length: ' + length + b'\r\n\r\n'
                client = self.reject([header, b'{}'])
                self.assertFalse(client.read_past_body)
                self.assertEqual(client.chunks, [])
                self.assertTrue(client.response.startswith(b'HTTP/1.1 503 '))

    def test_overload_rejects_ambiguous_framing_without_reading_body(self):
        for framing in (
            b'Content-Length: 2\r\nContent-Length: 2\r\n',
            b'Content-Length: 0\r\nContent-Length: 0\r\n',
            b'Content-Length: 100\r\ncontent-length: 2\r\n',
            b'Content-Length: 2\r\nContent-Length: 100\r\n',
            b'Content-Length: 2\r\nContent-Length: invalid\r\n',
            b'Transfer-Encoding: chunked\r\nContent-Length: 100\r\n',
            b'Content-Length: 100\r\ntransfer-encoding: chunked\r\n',
            b'Transfer-Encoding: chunked\r\n',
            b'Content-Length: 2\r\nTransfer-Encoding:\r\n',
            b'Content-Length: 100\r\nContent-Length: 2\r\nExpect: 100-continue\r\n',
            b'Transfer-Encoding: chunked\r\nExpect: 100-continue\r\n',
        ):
            with self.subTest(framing=framing):
                header = b'POST /assess HTTP/1.1\r\n' + framing + b'\r\n'
                client = self.reject([header, b'{}'])
                self.assertFalse(client.read_past_body)
                self.assertEqual(client.chunks, [b'{}'])
                self.assertEqual(client.response, b'')

    def test_overload_preserves_requests_without_bodies(self):
        for header in (
            b'GET /health HTTP/1.1\r\nHost: localhost\r\n\r\n',
            b'POST /assess HTTP/1.1\r\ncOnTeNt-LeNgTh: 0\r\n\r\n',
        ):
            with self.subTest(header=header):
                client = self.reject([header])
                self.assertFalse(client.read_past_body)
                self.assertTrue(client.response.startswith(b'HTTP/1.1 503 '))


if __name__ == '__main__':
    unittest.main()
