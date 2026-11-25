use std::fs;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use pulse_edge::{append_cache, flush_cache, read_cache, SAMPLE_PATH};

struct TestCache(PathBuf);

impl TestCache {
    fn new() -> Self {
        let timestamp = SystemTime::now().duration_since(UNIX_EPOCH).unwrap();
        Self(std::env::temp_dir().join(format!(
            "pulse-replay-{}-{}.jsonl",
            std::process::id(),
            timestamp.as_nanos()
        )))
    }

    fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for TestCache {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

fn read_request(stream: &mut TcpStream) -> String {
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .unwrap();
    let mut reader = BufReader::new(stream);
    let mut line = String::new();
    reader.read_line(&mut line).unwrap();
    assert!(line.starts_with("POST /api/v1/samples HTTP/1.1"));
    let mut content_length = None;
    loop {
        line.clear();
        assert!(reader.read_line(&mut line).unwrap() > 0);
        if line == "\r\n" {
            break;
        }
        if let Some(raw) = line.strip_prefix("Content-Length: ") {
            content_length = Some(raw.trim().parse::<usize>().unwrap());
        }
    }
    let mut body = vec![0; content_length.unwrap()];
    reader.read_exact(&mut body).unwrap();
    String::from_utf8(body).unwrap()
}

#[test]
fn appending_during_replay_preserves_new_records() {
    let cache = TestCache::new();
    let initial = r#"{"event_id":"before-replay"}"#;
    let appended = r#"{"event_id":"during-replay"}"#;
    append_cache(cache.path(), initial).unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap().to_string();
    let writer_path = cache.0.clone();
    let worker = thread::spawn(move || {
        let (mut stream, _) = listener.accept().unwrap();
        assert_eq!(read_request(&mut stream), initial);
        append_cache(&writer_path, appended).unwrap();
        stream
            .write_all(b"HTTP/1.1 201 Created\r\nContent-Length: 0\r\n\r\n")
            .unwrap();
    });
    let acknowledged = flush_cache(&address, cache.path());
    worker.join().unwrap();
    assert_eq!(acknowledged, 1);
    assert_eq!(
        read_cache(cache.path()).unwrap(),
        vec![
            format!("{SAMPLE_PATH}\t{initial}"),
            format!("{SAMPLE_PATH}\t{appended}")
        ]
    );

    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap().to_string();
    let worker = thread::spawn(move || {
        for expected in [initial, appended] {
            let (mut stream, _) = listener.accept().unwrap();
            assert_eq!(read_request(&mut stream), expected);
            stream
                .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
                .unwrap();
        }
    });
    let acknowledged = flush_cache(&address, cache.path());
    worker.join().unwrap();
    assert_eq!(acknowledged, 2);
    assert!(!cache.path().exists());
}
