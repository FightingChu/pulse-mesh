use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
use std::net::TcpStream;
use std::path::Path;
use std::time::Duration;

pub const DEFAULT_ADDR: &str = "127.0.0.1:8080";
pub const DEFAULT_CACHE: &str = "cache.jsonl";
pub const SAMPLE_PATH: &str = "/api/v1/samples";
pub const DIAGNOSE_PATH: &str = "/api/v1/diagnose";
const MAX_CACHE_BYTES: u64 = 64 * 1024 * 1024;
const MAX_RECORD_BYTES: usize = 128 * 1024;

pub struct Sample<'a> {
    pub device_id: &'a str,
    pub metric: &'a str,
    pub value: f64,
    pub level: &'a str,
    pub temp_c: f64,
}

impl Sample<'_> {
    pub fn to_json(&self) -> String {
        format!(
            "{{\"device_id\":\"{}\",\"metric\":\"{}\",\"value\":{:.2},\"level\":\"{}\",\"temp_c\":{:.1}}}",
            self.device_id, self.metric, self.value, self.level, self.temp_c
        )
    }
}

pub fn forward(addr: &str, body: &str) -> bool {
    forward_to(addr, SAMPLE_PATH, body)
}

pub fn forward_to(addr: &str, path: &str, body: &str) -> bool {
    if path != SAMPLE_PATH && path != DIAGNOSE_PATH {
        return false;
    }
    let request = format!(
        "POST {} HTTP/1.1\r\nHost: {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        path,
        addr,
        body.len(),
        body
    );
    match TcpStream::connect(addr) {
        Ok(mut stream) => {
            let _ = stream.set_read_timeout(Some(Duration::from_secs(5)));
            let _ = stream.set_write_timeout(Some(Duration::from_secs(5)));
            if stream.write_all(request.as_bytes()).is_err() {
                return false;
            }
            read_http_success(&mut stream)
        }
        Err(_) => false,
    }
}

fn read_http_success(stream: &mut TcpStream) -> bool {
    let mut response = Vec::with_capacity(256);
    let mut chunk = [0u8; 256];
    while response.len() < 8192 {
        match stream.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => {
                response.extend_from_slice(&chunk[..n]);
                if response.windows(4).any(|window| window == b"\r\n\r\n") {
                    break;
                }
            }
            Err(_) => return false,
        }
    }
    let Some(line_end) = response.windows(2).position(|window| window == b"\r\n") else {
        return false;
    };
    let Ok(status_line) = std::str::from_utf8(&response[..line_end]) else {
        return false;
    };
    let mut parts = status_line.split_whitespace();
    matches!(
        (parts.next(), parts.next().and_then(|code| code.parse::<u16>().ok())),
        (Some("HTTP/1.1" | "HTTP/1.0"), Some(200..=299))
    )
}

pub fn waveform(rotation_hz: f64, bearing_amplitude: f64, n: usize, sample_rate_hz: f64) -> Vec<f64> {
    if !rotation_hz.is_finite()
        || !bearing_amplitude.is_finite()
        || !sample_rate_hz.is_finite()
        || sample_rate_hz <= 0.0
    {
        return Vec::new();
    }
    let two_pi = std::f64::consts::TAU;
    (0..n)
        .map(|i| {
            let t = i as f64 / sample_rate_hz;
            let fundamental = (two_pi * rotation_hz * t).sin();
            let harmonic = 0.15 * (two_pi * 2.0 * rotation_hz * t).sin();
            let bearing = bearing_amplitude * (two_pi * 120.0 * t).sin();
            fundamental + harmonic + bearing
        })
        .collect()
}

pub fn diagnose_body(
    device_id: &str,
    values: &[f64],
    sample_rate_hz: f64,
    rotation_hz: f64,
) -> String {
    if !sample_rate_hz.is_finite() || sample_rate_hz <= 0.0 || !rotation_hz.is_finite() {
        return String::new();
    }
    let encoded: Vec<String> = values.iter().map(|v| format!("{:.4}", v)).collect();
    format!(
        "{{\"device_id\":\"{}\",\"values\":[{}],\"sample_rate_hz\":{},\"rotation_hz\":{}}}",
        device_id,
        encoded.join(","),
        sample_rate_hz,
        rotation_hz
    )
}

pub fn append_cache(path: &Path, body: &str) -> io::Result<()> {
    append_cache_to(path, SAMPLE_PATH, body)
}

pub fn append_cache_to(path: &Path, route: &str, body: &str) -> io::Result<()> {
    if route != SAMPLE_PATH && route != DIAGNOSE_PATH {
        return Err(io::Error::new(io::ErrorKind::InvalidInput, "unsupported cache route"));
    }
    if body.len() > MAX_RECORD_BYTES || body.contains(['\r', '\n']) {
        return Err(io::Error::new(io::ErrorKind::InvalidInput, "invalid or oversized cache record"));
    }
    let current_size = path.metadata().map(|meta| meta.len()).unwrap_or(0);
    let record_size = route.len() as u64 + body.len() as u64 + 2;
    if current_size.saturating_add(record_size) > MAX_CACHE_BYTES {
        return Err(io::Error::new(io::ErrorKind::WriteZero, "offline cache limit reached"));
    }
    let mut file = OpenOptions::new().create(true).append(true).open(path)?;
    writeln!(file, "{}\t{}", route, body)?;
    file.sync_data()
}

pub fn read_cache(path: &Path) -> io::Result<Vec<String>> {
    if !path.exists() {
        return Ok(Vec::new());
    }
    let content = fs::read_to_string(path)?;
    Ok(content.lines().map(String::from).collect())
}

pub fn flush_cache(addr: &str, path: &Path) -> usize {
    let pending = match fs::read_to_string(path) {
        Ok(content) => content,
        Err(_) => return 0,
    };
    let lines: Vec<&str> = pending.lines().collect();
    if lines.is_empty() {
        let _ = fs::remove_file(path);
        return 0;
    }
    let mut sent = 0;
    for line in &lines {
        let (route, body) = line
            .split_once('\t')
            .unwrap_or((SAMPLE_PATH, *line));
        if body.len() > MAX_RECORD_BYTES || (route != SAMPLE_PATH && route != DIAGNOSE_PATH) {
            return sent;
        }
        if !forward_to(addr, route, body) {
            return sent;
        }
        sent += 1;
    }
    if sent == lines.len() && sent > 0 {
        let completed = path.with_extension("jsonl.sent");
        if fs::rename(path, &completed).is_ok() {
            let _ = fs::remove_file(completed);
        }
    }
    sent
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn temp_path(name: &str) -> PathBuf {
        let mut p = std::env::temp_dir();
        p.push(format!("pulse-edge-{}-{}", std::process::id(), name));
        let _ = fs::remove_file(&p);
        p
    }

    #[test]
    fn body_keeps_edge_verdict() {
        let body = Sample {
            device_id: "motor-01",
            metric: "vibration_rms",
            value: 1.234,
            level: "normal",
            temp_c: 55.5,
        }
        .to_json();
        assert_eq!(
            body,
            "{\"device_id\":\"motor-01\",\"metric\":\"vibration_rms\",\"value\":1.23,\"level\":\"normal\",\"temp_c\":55.5}"
        );
    }

    #[test]
    fn cache_roundtrip() {
        let path = temp_path("roundtrip");
        append_cache(&path, "{\"a\":1}").unwrap();
        append_cache(&path, "{\"b\":2}").unwrap();
        let lines = read_cache(&path).unwrap();
        assert_eq!(
            lines,
            vec![
                format!("{}\t{{\"a\":1}}", SAMPLE_PATH),
                format!("{}\t{{\"b\":2}}", SAMPLE_PATH)
            ]
        );
        assert!(path.exists(), "reading must not consume the outbox");
        let _ = fs::remove_file(path);
    }

    #[test]
    fn take_missing_cache_is_empty() {
        let path = temp_path("missing");
        assert!(read_cache(&path).unwrap().is_empty());
    }

    #[test]
    fn flush_keeps_unsent_lines() {
        let path = temp_path("flush");
        append_cache(&path, "{\"a\":1}").unwrap();
        let sent = flush_cache("127.0.0.1:1", &path);
        assert_eq!(sent, 0);
        let kept = fs::read_to_string(&path).unwrap();
        assert!(kept.contains("{\"a\":1}"));
        let _ = fs::remove_file(&path);
    }

    #[test]
    fn flush_removes_cache_after_successful_ack() {
        use std::io::{Read as _, Write as _};
        use std::net::TcpListener;
        use std::thread;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap().to_string();
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 512];
            let _ = stream.read(&mut buf).unwrap();
            stream.write_all(b"HTTP/1.1 201 Created\r\nContent-Length: 0\r\n\r\n").unwrap();
        });

        let path = temp_path("flush-success");
        append_cache(&path, "{\"a\":1}").unwrap();
        assert_eq!(flush_cache(&addr, &path), 1);
        handle.join().unwrap();
        assert!(!path.exists(), "acknowledged outbox should be removed");
    }

    #[test]
    fn server_error_does_not_ack_forward() {
        use std::io::Write as _;
        use std::net::TcpListener;
        use std::thread;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap().to_string();
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream.write_all(b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n").unwrap();
        });
        assert!(!forward_to(&addr, SAMPLE_PATH, "{}"));
        handle.join().unwrap();
    }

    #[test]
    fn flush_preserves_diagnosis_route() {
        use std::io::{Read as _, Write as _};
        use std::net::TcpListener;
        use std::thread;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap().to_string();
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 1024];
            let n = stream.read(&mut buf).unwrap();
            stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n").unwrap();
            String::from_utf8_lossy(&buf[..n]).to_string()
        });

        let path = temp_path("flush-route");
        append_cache_to(&path, DIAGNOSE_PATH, "{\"values\":[]}").unwrap();
        assert_eq!(flush_cache(&addr, &path), 1);
        let request = handle.join().unwrap();
        assert!(request.starts_with("POST /api/v1/diagnose HTTP/1.1"));
    }

    #[test]
    fn waveform_has_expected_shape() {
        let samples = waveform(25.0, 0.6, 1024, 1000.0);
        assert_eq!(samples.len(), 1024);
        let peak = samples.iter().cloned().fold(f64::MIN, f64::max);
        assert!(peak > 1.0 && peak < 3.0, "peak = {}", peak);
    }

    #[test]
    fn invalid_waveform_configuration_returns_empty() {
        assert!(waveform(25.0, 0.6, 1024, 0.0).is_empty());
        assert!(waveform(f64::NAN, 0.6, 1024, 1000.0).is_empty());
    }

    #[test]
    fn diagnose_body_encodes_all_values() {
        let body = diagnose_body("motor-01", &[0.0, 0.5, -0.5], 1000.0, 25.0);
        assert!(body.starts_with("{\"device_id\":\"motor-01\",\"values\":["));
        assert!(body.contains("0.0000,0.5000,-0.5000"));
        assert!(body.ends_with("],\"sample_rate_hz\":1000,\"rotation_hz\":25}"));
    }

    #[test]
    fn forward_to_uses_requested_path() {
        use std::io::{Read as _, Write as _};
        use std::net::TcpListener;
        use std::thread;

        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let addr = listener.local_addr().unwrap().to_string();
        let handle = thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let mut buf = [0u8; 512];
            let n = stream.read(&mut buf).unwrap();
            stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n").unwrap();
            String::from_utf8_lossy(&buf[..n]).to_string()
        });

        assert!(forward_to(&addr, DIAGNOSE_PATH, "{\"a\":1}"));
        let request = handle.join().unwrap();
        assert!(request.starts_with("POST /api/v1/diagnose HTTP/1.1"), "{}", request);
    }

    #[test]
    fn rejects_unrecognized_route_before_connecting() {
        assert!(!forward_to("127.0.0.1:1", "/admin", "{}"));
    }

    #[test]
    fn cache_rejects_invalid_route_and_multiline_body() {
        let path = temp_path("invalid-record");
        assert!(append_cache_to(&path, "/admin", "{}").is_err());
        assert!(append_cache(&path, "{}\n{}").is_err());
        assert!(!path.exists());
    }
}
