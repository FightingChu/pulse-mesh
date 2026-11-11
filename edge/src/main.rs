use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};
use std::thread;
use std::time::Duration;

use pulse_edge::{
    append_cache_to, batch_body, diagnose_body, flush_cache, forward_to, waveform, OwnedSample,
    BATCH_PATH, DEFAULT_ADDR, DEFAULT_CACHE, DIAGNOSE_PATH,
};

fn level_of(value: f64) -> &'static str {
    if value > 4.0 {
        "critical"
    } else if value > 2.5 {
        "warning"
    } else {
        "normal"
    }
}

fn main() {
    let addr = std::env::var("PULSE_CLOUD_ADDR").unwrap_or_else(|_| DEFAULT_ADDR.to_string());
    let cache = Path::new(DEFAULT_CACHE);
    let session_id = session_id();

    let replayed = flush_cache(&addr, cache);
    if replayed > 0 {
        println!("replayed {} cached samples", replayed);
    }

    let mut samples = Vec::with_capacity(10);
    for tick in 0..10 {
        let value = if tick == 7 { 4.8 } else { 1.2 };
        let event_id = format!("{session_id}-sample-{tick}");
        samples.push(OwnedSample {
            event_id,
            device_id: "motor-01".to_string(),
            metric: "vibration_rms".to_string(),
            value,
            level: level_of(value).to_string(),
            temp_c: 55.0 + (tick % 10) as f64,
        });
    }
    let batch = batch_body(&samples);
    if forward_to(&addr, BATCH_PATH, &batch) {
        println!("forwarded batch samples={}", samples.len());
    } else {
        append_cache_to(cache, BATCH_PATH, &batch).expect("cache offline sample batch");
        println!("offline cached batch samples={}", samples.len());
    }

    thread::sleep(Duration::from_millis(200));
    let values = waveform(25.0, 0.6, 1024, 1000.0);
    let diagnosis_event_id = format!("{session_id}-spectrum-0");
    let body = diagnose_body(&diagnosis_event_id, "motor-01", &values, 1000.0, 25.0);
    if forward_to(&addr, DIAGNOSE_PATH, &body) {
        println!("diagnose sent: {} points", values.len());
    } else {
        append_cache_to(cache, DIAGNOSE_PATH, &body).expect("cache offline diagnosis");
        println!("diagnose failed: cloud unavailable");
    }
}

fn session_id() -> String {
    static COUNTER: AtomicU64 = AtomicU64::new(0);
    format!("{}-{}-{}", std::process::id(), std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|elapsed| elapsed.as_nanos())
        .unwrap_or_default(), COUNTER.fetch_add(1, Ordering::Relaxed))
}
