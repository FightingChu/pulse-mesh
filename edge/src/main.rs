use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};
use std::thread;

use pulse_edge::{
    append_cache_to, batch_body, diagnose_body, flush_cache, forward_to, waveform, EdgeConfig,
    OwnedSample, BATCH_PATH, DIAGNOSE_PATH,
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
    let config = EdgeConfig::from_env().unwrap_or_else(|error| {
        eprintln!("invalid edge configuration: {error}");
        std::process::exit(2);
    });
    let cache = Path::new(&config.cache_path);
    let session_id = session_id();

    let replayed = flush_cache(&config.cloud_addr, cache);
    if replayed > 0 {
        println!("replayed {} cached samples", replayed);
    }

    let mut samples = Vec::with_capacity(config.sample_count);
    for tick in 0..config.sample_count {
        let value = if tick == config.sample_count * 7 / 10 { 4.8 } else { 1.2 };
        let event_id = format!("{session_id}-sample-{tick}");
        samples.push(OwnedSample {
            event_id,
            device_id: config.device_id.clone(),
            metric: "vibration_rms".to_string(),
            value,
            level: level_of(value).to_string(),
            temp_c: 55.0 + (tick % 10) as f64,
        });
        if tick + 1 < config.sample_count {
            thread::sleep(config.sample_interval);
        }
    }
    let batch = batch_body(&samples);
    if forward_to(&config.cloud_addr, BATCH_PATH, &batch) {
        println!("forwarded batch samples={}", samples.len());
    } else {
        append_cache_to(cache, BATCH_PATH, &batch).expect("cache offline sample batch");
        println!("offline cached batch samples={}", samples.len());
    }

    let values = waveform(
        config.rotation_hz,
        config.bearing_amplitude,
        1024,
        config.sample_rate_hz,
    );
    let diagnosis_event_id = format!("{session_id}-spectrum-0");
    let body = diagnose_body(
        &diagnosis_event_id,
        &config.device_id,
        &values,
        config.sample_rate_hz,
        config.rotation_hz,
    );
    if forward_to(&config.cloud_addr, DIAGNOSE_PATH, &body) {
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
