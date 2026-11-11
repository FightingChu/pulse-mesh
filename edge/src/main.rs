use std::path::Path;
use std::thread;
use std::time::Duration;

use pulse_edge::{
    append_cache, diagnose_body, flush_cache, forward, forward_to, waveform, Sample,
    DEFAULT_ADDR, DEFAULT_CACHE, DIAGNOSE_PATH,
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

    let replayed = flush_cache(&addr, cache);
    if replayed > 0 {
        println!("replayed {} cached samples", replayed);
    }

    for tick in 0..10 {
        let value = if tick == 7 { 4.8 } else { 1.2 };
        let body = Sample {
            device_id: "motor-01",
            metric: "vibration_rms",
            value,
            level: level_of(value),
            temp_c: 55.0 + (tick % 10) as f64,
        }
        .to_json();
        if forward(&addr, &body) {
            println!("forwarded tick={} value={:.2}", tick, value);
        } else {
            append_cache(cache, &body).expect("write offline cache");
            println!("offline cached tick={} value={:.2}", tick, value);
        }
        thread::sleep(Duration::from_millis(200));
    }

    let values = waveform(25.0, 0.6, 1024, 1000.0);
    let body = diagnose_body("motor-01", &values, 1000.0, 25.0);
    if forward_to(&addr, DIAGNOSE_PATH, &body) {
        println!("diagnose sent: {} points", values.len());
    } else {
        println!("diagnose failed: cloud unavailable");
    }
}
