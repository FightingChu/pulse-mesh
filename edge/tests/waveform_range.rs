use pulse_edge::{diagnose_body, waveform, EdgeConfig};

#[test]
fn accepted_large_frequencies_produce_finite_waveforms() {
    for rotation in ["1e307", "2e307", "4e307"] {
        let config = EdgeConfig::from_lookup(|key| match key {
            "PULSE_SAMPLE_RATE_HZ" => Some("1e308".to_string()),
            "PULSE_ROTATION_HZ" => Some(rotation.to_string()),
            "PULSE_BEARING_AMPLITUDE" => Some("0".to_string()),
            _ => None,
        })
        .unwrap();
        let values = waveform(
            config.rotation_hz,
            config.bearing_amplitude,
            1024,
            config.sample_rate_hz,
        );
        assert_eq!(values.len(), 1024);
        let equivalent_rotation = config.rotation_hz / config.sample_rate_hz * 1000.0;
        let reference = waveform(equivalent_rotation, 0.0, 1024, 1000.0);
        for (index, (actual, expected)) in values.iter().zip(reference).enumerate() {
            assert!(actual.is_finite(), "rotation={rotation} sample={index}");
            assert!(
                (actual - expected).abs() < 1e-10,
                "rotation={rotation} sample={index}: {actual} != {expected}"
            );
        }
        let body = diagnose_body(
            "range-test",
            "motor-01",
            &values,
            config.sample_rate_hz,
            config.rotation_hz,
        );
        assert!(!body.contains("NaN") && !body.contains("inf"));
    }
}

#[test]
fn ordinary_waveform_keeps_existing_signal_components() {
    for (rotation, amplitude, rate) in [(25.0, 0.6, 1000.0), (30.0, 0.3, 2048.0), (0.0, 0.0, 240.0)]
    {
        let values = waveform(rotation, amplitude, 1024, rate);
        assert_eq!(values.len(), 1024);
        for (index, actual) in values.iter().enumerate() {
            let time = index as f64 / rate;
            let expected = (std::f64::consts::TAU * rotation * time).sin()
                + 0.15 * (std::f64::consts::TAU * 2.0 * rotation * time).sin()
                + amplitude * (std::f64::consts::TAU * 120.0 * time).sin();
            assert!(
                (actual - expected).abs() < 1e-12,
                "sample={index}: {actual} != {expected}"
            );
        }
    }
}
