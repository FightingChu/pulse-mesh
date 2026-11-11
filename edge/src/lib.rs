use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
use std::net::TcpStream;
use std::path::Path;

pub const DEFAULT_ADDR: &str = "127.0.0.1:8080";
pub const DEFAULT_CACHE: &str = "cache.jsonl";
pub const SAMPLE_PATH: &str = "/api/v1/samples";
pub const DIAGNOSE_PATH: &str = "/api/v1/diagnose";

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
    let request = format!(
        "POST {} HTTP/1.1\r\nHost: {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
        path,
        addr,
        body.len(),
        body
    );
    match TcpStream::connect(addr) {
        Ok(mut stream) => {
            if stream.write_all(request.as_bytes()).is_err() {
                return false;
            }
            let mut buf = [0u8; 64];
            matches!(stream.read(&mut buf), Ok(n) if n > 0)
        }
        Err(_) => false,
    }
}

pub fn waveform(rotation_hz: f64, bearing_amplitude: f64, n: usize, sample_rate_hz: f64) -> Vec<f64> {
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
    let mut file = OpenOptions::new().create(true).append(true).open(path)?;
    writeln!(file, "{}", body)
}

pub fn take_cache(path: &Path) -> io::Result<Vec<String>> {
    if !path.exists() {
        return Ok(Vec::new());
    }
    let content = fs::read_to_string(path)?;
    fs::remove_file(path)?;
    Ok(content
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .map(String::from)
        .collect())
}

pub fn flush_cache(addr: &str, path: &Path) -> usize {
    let pending = take_cache(path).unwrap_or_default();
    let mut sent = 0;
    let mut keep = Vec::new();
    for body in pending {
        if forward(addr, &body) {
            sent += 1;
        } else {
            keep.push(body);
        }
    }
    if !keep.is_empty() {
        let _ = fs::write(path, keep.join("\n") + "\n");
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
        let lines = take_cache(&path).unwrap();
        assert_eq!(lines, vec!["{\"a\":1}", "{\"b\":2}"]);
        assert!(!path.exists(), "cache file should be consumed");
    }

    #[test]
    fn take_missing_cache_is_empty() {
        let path = temp_path("missing");
        assert!(take_cache(&path).unwrap().is_empty());
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
    fn waveform_has_expected_shape() {
        let samples = waveform(25.0, 0.6, 1024, 1000.0);
        assert_eq!(samples.len(), 1024);
        let peak = samples.iter().cloned().fold(f64::MIN, f64::max);
        assert!(peak > 1.0 && peak < 3.0, "peak = {}", peak);
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
}
