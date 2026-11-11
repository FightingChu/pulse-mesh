use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
use std::net::TcpStream;
use std::path::Path;

pub const DEFAULT_ADDR: &str = "127.0.0.1:8080";
pub const DEFAULT_CACHE: &str = "cache.jsonl";

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
    let request = format!(
        "POST /api/v1/samples HTTP/1.1\r\nHost: {}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{}",
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
}
