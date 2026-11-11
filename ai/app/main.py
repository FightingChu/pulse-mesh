import json, math, os, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

import spectrum

MIN_HISTORY = 3
SD_FLOOR = 1e-6
WARN_Z = 3.0
CRITICAL_Z = 6.0
MAX_BODY_BYTES = 128 * 1024
MAX_WAVEFORM_VALUES = 1024

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

def assess(device_id, metric, values):
    if len(values) > 10000 or any(not math.isfinite(v) for v in values):
        raise ValueError('values must be finite and contain at most 10000 points')
    last = values[-1] if values else 0.0
    z = zscore(last, baseline(values))
    return {'device_id': device_id, 'metric': metric, 'last': last, 'rms': rms(values), 'z': round(z, 3), 'level': level_of(z)}

def diagnose(device_id, values, sample_rate_hz=1000.0, rotation_hz=25.0, event_id=''):
    if len(values) != MAX_WAVEFORM_VALUES:
        raise ValueError('values must contain exactly 1024 samples')
    if not math.isfinite(sample_rate_hz) or not math.isfinite(rotation_hz):
        raise ValueError('sample and rotation frequencies must be finite')
    if sample_rate_hz <= 0 or rotation_hz < 0 or sample_rate_hz < 2 * rotation_hz:
        raise ValueError('invalid sample_rate_hz or rotation_hz')
    if any(not math.isfinite(v) for v in values):
        raise ValueError('waveform values must be finite numbers')
    result = spectrum.diagnose(values, sample_rate_hz, rotation_hz)
    result['device_id'] = device_id
    result['event_id'] = event_id
    return result

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        if urlparse(self.path).path == '/health':
            self._send({'status': 'ok'})
        else:
            self._send(assess('motor-01', 'vibration_rms', [1.1, 1.2, 1.0, 1.3, 1.2, 4.8]))
    def do_POST(self):
        try:
            n = int(self.headers.get('Content-Length', 0))
            if n < 0 or n > MAX_BODY_BYTES:
                self.close_connection = True
                self._send({'error': 'request body too large'}, 413)
                return
            raw = self.rfile.read(n)
            text = (raw or b'{}').decode('utf-8')
            decoder = json.JSONDecoder(parse_constant=lambda value: (_ for _ in ()).throw(ValueError('invalid numeric constant: ' + value)))
            body, end = decoder.raw_decode(text.lstrip())
            if text.lstrip()[end:].strip():
                raise ValueError('request must contain a single JSON value')
            if not isinstance(body, dict):
                raise ValueError('request body must be a JSON object')
            path = urlparse(self.path).path
            if path == '/spectrum':
                result = diagnose(
                    body.get('device_id', 'unknown'),
                    body.get('values', []),
                    float(body.get('sample_rate_hz', 1000.0)),
                    float(body.get('rotation_hz', 25.0)),
                    body.get('event_id', ''),
                )
                self._send(result)
                return
            self._send(assess(body.get('device_id', 'unknown'), body.get('metric', 'vibration_rms'), body.get('values', [])))
        except (ValueError, TypeError, json.JSONDecodeError) as exc:
            self._send({'error': str(exc)}, 400)
    def _send(self, obj, status=200):
        data = json.dumps(obj).encode()
        self.send_response(status); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(data))); self.end_headers(); self.wfile.write(data)
    def log_message(self, *a):
        if os.environ.get('PULSE_ACCESS_LOG', '1') == '0':
            return
        sys.stderr.write('%s %s\n' % (time.strftime('%Y-%m-%dT%H:%M:%S'), a[0] % a[1:]))

if __name__ == '__main__':
    server = ThreadingHTTPServer(('0.0.0.0', 8000), H)
    server.daemon_threads = True
    print('pulse-mesh ai listening :8000', flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
