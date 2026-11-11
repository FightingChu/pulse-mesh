import json, math, os, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

import spectrum

MIN_HISTORY = 3
SD_FLOOR = 1e-6
WARN_Z = 3.0
CRITICAL_Z = 6.0

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
    last = values[-1] if values else 0.0
    z = zscore(last, baseline(values))
    return {'device_id': device_id, 'metric': metric, 'last': last, 'rms': rms(values), 'z': round(z, 3), 'level': level_of(z)}

def diagnose(device_id, values, sample_rate_hz=1000.0, rotation_hz=25.0):
    result = spectrum.diagnose(values, sample_rate_hz, rotation_hz)
    result['device_id'] = device_id
    return result

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        if urlparse(self.path).path == '/health':
            self._send({'status': 'ok'})
        else:
            self._send(assess('motor-01', 'vibration_rms', [1.1, 1.2, 1.0, 1.3, 1.2, 4.8]))
    def do_POST(self):
        n = int(self.headers.get('Content-Length', 0))
        body = json.loads(self.rfile.read(n) or b'{}')
        path = urlparse(self.path).path
        if path == '/spectrum':
            try:
                result = diagnose(
                    body.get('device_id', 'unknown'),
                    body.get('values', []),
                    float(body.get('sample_rate_hz', 1000.0)),
                    float(body.get('rotation_hz', 25.0)),
                )
            except (ValueError, TypeError) as exc:
                self._send({'error': str(exc)}, 400)
                return
            self._send(result)
            return
        self._send(assess(body.get('device_id', 'unknown'), body.get('metric', 'vibration_rms'), body.get('values', [])))
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
