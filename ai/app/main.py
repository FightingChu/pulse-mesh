import json, math, os, sys, time
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

import spectrum

MIN_HISTORY = 3
SD_FLOOR = 1e-6
WARN_Z = 3.0
CRITICAL_Z = 6.0
MAX_BODY_BYTES = 128 * 1024
MAX_WAVEFORM_VALUES = 1024
MIN_DIAGNOSIS_SAMPLE_RATE_HZ = 240.0
DEFAULT_MAX_WORKERS = 8
MAX_WORKERS_LIMIT = 64
REQUEST_TIMEOUT_SECONDS = 5

def rms(values):
    return math.sqrt(sum(v*v for v in values)/len(values)) if values else 0.0

def baseline(values):
    return values[:-1]

def zscore(value, history):
    if len(history) < MIN_HISTORY:
        return 0.0
    mean = sum(history)/len(history)
    sd = math.sqrt(sum((v-mean)**2 for v in history)/len(history))
    return (value-mean)/max(sd, SD_FLOOR)

def level_of(z):
    if abs(z) >= CRITICAL_Z:
        return 'critical'
    if abs(z) >= WARN_Z:
        return 'warning'
    return 'normal'

def is_finite_number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False

def assess(device_id, metric, values):
    if not isinstance(values, list) or len(values) > 10000 or any(not is_finite_number(value) for value in values):
        raise ValueError('values must be an array of at most 10000 finite numbers')
    last = values[-1] if values else 0.0
    try:
        z = zscore(last, baseline(values))
        rms_value = rms(values)
    except OverflowError as exc:
        raise ValueError('assessment exceeds numeric range') from exc
    if not math.isfinite(z) or not math.isfinite(rms_value):
        raise ValueError('assessment exceeds numeric range')
    return {'device_id': device_id, 'metric': metric, 'last': last, 'rms': rms_value, 'z': round(z, 3), 'level': level_of(z)}

def diagnose(device_id, values, sample_rate_hz=1000.0, rotation_hz=25.0, event_id=''):
    if not isinstance(device_id, str) or not device_id or len(device_id) > 64:
        raise ValueError('device_id must be a non-empty string of at most 64 characters')
    if not isinstance(event_id, str) or len(event_id) > 128:
        raise ValueError('event_id must be a string of at most 128 characters')
    if not isinstance(values, list) or len(values) != MAX_WAVEFORM_VALUES:
        raise ValueError('values must be an array of exactly 1024 samples')
    if not is_finite_number(sample_rate_hz) or not is_finite_number(rotation_hz):
        raise ValueError('sample and rotation frequencies must be finite numbers')
    if sample_rate_hz < MIN_DIAGNOSIS_SAMPLE_RATE_HZ or rotation_hz < 0 or sample_rate_hz < 2 * rotation_hz:
        raise ValueError('invalid sample_rate_hz or rotation_hz')
    if any(not is_finite_number(value) for value in values):
        raise ValueError('waveform values must be finite numbers')
    result = spectrum.diagnose(values, sample_rate_hz, rotation_hz)
    result['device_id'] = device_id
    result['event_id'] = event_id
    return result

def _parse_content_length(value):
    value = value.strip(' \t')
    if not value.isascii() or not value.isdecimal():
        raise ValueError('invalid content-length')
    return int(value)

class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def do_GET(self):
        if urlparse(self.path).path in ('/health', '/health/live'):
            self._send({'status': 'ok'})
        else:
            self._send({'error': 'not found'}, 404)
    def do_POST(self):
        try:
            if self.headers.get_all('Transfer-Encoding'):
                self.close_connection = True
                self._send({'error': 'transfer encoding is not supported'}, 400)
                return
            content_lengths = self.headers.get_all('Content-Length') or []
            if len(content_lengths) != 1:
                self.close_connection = True
                self._send({'error': 'exactly one content-length header is required'}, 411)
                return
            try:
                n = _parse_content_length(content_lengths[0])
            except ValueError:
                self.close_connection = True
                self._send({'error': 'invalid content-length'}, 400)
                return
            if n > MAX_BODY_BYTES:
                self.close_connection = True
                self._send({'error': 'request body too large'}, 413)
                return
            raw = self.rfile.read(n)
            if len(raw) != n:
                self.close_connection = True
                self._send({'error': 'incomplete request body'}, 400)
                return
            text = (raw or b'{}').decode('utf-8')
            decoder = json.JSONDecoder(parse_constant=lambda value: (_ for _ in ()).throw(ValueError('invalid numeric constant: ' + value)))
            body = decoder.decode(text)
            if not isinstance(body, dict):
                raise ValueError('request body must be a JSON object')
            path = urlparse(self.path).path
            if path == '/spectrum':
                result = diagnose(
                    body.get('device_id', 'unknown'),
                    body.get('values', []),
                    body.get('sample_rate_hz', 1000.0),
                    body.get('rotation_hz', 25.0),
                    body.get('event_id', ''),
                )
                self._send(result)
                return
            if path == '/assess':
                self._send(assess(body.get('device_id', 'unknown'), body.get('metric', 'vibration_rms'), body.get('values', [])))
                return
            self._send({'error': 'not found'}, 404)
        except (ValueError, TypeError, json.JSONDecodeError) as exc:
            self._send({'error': str(exc)}, 400)
    def _send(self, obj, status=200):
        data = json.dumps(obj, allow_nan=False).encode()
        self.send_response(status); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)
    def log_message(self, *a):
        if os.environ.get('PULSE_ACCESS_LOG', '1') == '0':
            return
        sys.stderr.write('%s %s\n' % (time.strftime('%Y-%m-%dT%H:%M:%S'), a[0] % a[1:]))


class BoundedThreadingHTTPServer(ThreadingHTTPServer):
    request_queue_size = 16
    daemon_threads = True

    def __init__(
        self,
        server_address,
        handler_class,
        max_workers=DEFAULT_MAX_WORKERS,
        request_timeout=REQUEST_TIMEOUT_SECONDS,
        max_rejection_workers=16,
    ):
        if max_workers < 1 or max_workers > MAX_WORKERS_LIMIT:
            raise ValueError('max_workers must be between 1 and 64')
        if max_rejection_workers < 1 or max_rejection_workers > self.request_queue_size:
            raise ValueError('max_rejection_workers must be between 1 and 16')
        self._worker_slots = threading.BoundedSemaphore(max_workers)
        self._rejection_slots = threading.BoundedSemaphore(max_rejection_workers)
        self._rejection_threads = set()
        self._rejection_threads_lock = threading.Lock()
        self._request_timeout = request_timeout
        super().__init__(server_address, handler_class)

    def get_request(self):
        request, address = super().get_request()
        request.settimeout(self._request_timeout)
        return request, address

    def process_request(self, request, client_address):
        if not self._worker_slots.acquire(blocking=False):
            if not self._rejection_slots.acquire(blocking=False):
                self.shutdown_request(request)
                return
            try:
                thread = threading.Thread(
                    target=self._reject_overloaded_request,
                    args=(request,),
                    daemon=True,
                )
                with self._rejection_threads_lock:
                    self._rejection_threads.add(thread)
                thread.start()
            except Exception:
                self._rejection_slots.release()
                with self._rejection_threads_lock:
                    self._rejection_threads.discard(thread)
                self.shutdown_request(request)
                raise
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self._worker_slots.release()
            raise

    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self._worker_slots.release()

    def server_close(self):
        super().server_close()
        with self._rejection_threads_lock:
            threads = tuple(self._rejection_threads)
        for thread in threads:
            thread.join(timeout=self._request_timeout + 1)

    def _reject_overloaded_request(self, request):
        try:
            header = bytearray()
            header_end = -1
            while len(header) < 16 * 1024 and header_end < 0:
                chunk = request.recv(min(1024, 16 * 1024 - len(header)))
                if not chunk:
                    return
                header.extend(chunk)
                header_end = header.find(b'\r\n\r\n')
            if header_end < 0:
                return

            body_start = header_end + 4
            request_headers = header[:body_start]
            content_length = self._request_content_length(request_headers)
            if content_length is None:
                return
            if self._request_expects_continue(request_headers):
                self._send_busy_response(request)
                return

            if content_length > MAX_BODY_BYTES:
                return
            remaining = max(0, content_length - (len(header) - body_start))
            while remaining > 0:
                chunk = request.recv(min(4096, remaining))
                if not chunk:
                    return
                remaining -= len(chunk)

            self._send_busy_response(request)
        except OSError:
            pass
        finally:
            self.shutdown_request(request)
            self._rejection_slots.release()
            with self._rejection_threads_lock:
                self._rejection_threads.discard(threading.current_thread())

    @staticmethod
    def _request_expects_continue(header):
        for line in bytes(header).split(b'\r\n')[1:]:
            if not line:
                break
            name, separator, value = line.partition(b':')
            if separator and name.strip().lower() == b'expect':
                return value.strip().lower() == b'100-continue'
        return False

    @staticmethod
    def _send_busy_response(request):
        body = b'{"error":"AI service busy"}'
        request.sendall(
            b'HTTP/1.1 503 Service Unavailable\r\n'
            b'Content-Type: application/json\r\n'
            b'Retry-After: 1\r\n'
            b'Connection: close\r\n'
            + ('Content-Length: %d\r\n\r\n' % len(body)).encode()
            + body
        )

    @staticmethod
    def _request_content_length(header):
        lines = bytes(header).split(b'\r\n')[1:]
        length = None
        for line in lines:
            if not line:
                break
            name, separator, value = line.partition(b':')
            name = name.strip().lower()
            if separator and name == b'transfer-encoding':
                return None
            if separator and name == b'content-length':
                if length is not None:
                    return None
                try:
                    length = _parse_content_length(value.decode('latin-1'))
                except ValueError:
                    return None
        return length if length is not None else 0

if __name__ == '__main__':
    try:
        max_workers = int(os.environ.get('PULSE_AI_MAX_WORKERS', DEFAULT_MAX_WORKERS))
        server = BoundedThreadingHTTPServer(('0.0.0.0', 8000), H, max_workers=max_workers)
    except ValueError as exc:
        raise SystemExit('invalid AI server configuration: %s' % exc)
    print('pulse-mesh ai listening :8000', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
