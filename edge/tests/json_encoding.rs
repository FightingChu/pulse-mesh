use pulse_edge::{batch_body, diagnose_body, OwnedSample, Sample};

#[test]
fn sample_encoders_escape_strings_and_preserve_unicode() {
    let value = "电机\"\\\n\r\t\u{0008}\u{000c}\u{0000}";
    let escaped = r#"电机\"\\\n\r\t\b\f\u0000"#;
    let expected = format!(
        "{{\"event_id\":\"{escaped}\",\"device_id\":\"{escaped}\",\"metric\":\"{escaped}\",\"value\":1.25,\"level\":\"{escaped}\",\"temp_c\":55.5}}"
    );
    let sample = Sample {
        event_id: value,
        device_id: value,
        metric: value,
        value: 1.25,
        level: value,
        temp_c: 55.5,
    };
    assert_eq!(sample.to_json(), expected);
    let owned = OwnedSample {
        event_id: value.to_string(),
        device_id: value.to_string(),
        metric: value.to_string(),
        value: 1.25,
        level: value.to_string(),
        temp_c: 55.5,
    };
    assert_eq!(owned.to_json(), expected);
    assert_eq!(batch_body(&[owned]), format!("[{expected}]"));
}

#[test]
fn diagnosis_encodes_identities_without_injecting_fields() {
    let body = diagnose_body(
        "event\",\"values\":[999],\"extra\":\"",
        "电机\\east\n",
        &[0.0, -0.5],
        1000.0,
        25.0,
    );
    assert_eq!(
        body,
        r#"{"event_id":"event\",\"values\":[999],\"extra\":\"","device_id":"电机\\east\n","values":[0.0000,-0.5000],"sample_rate_hz":1000,"rotation_hz":25}"#
    );
}

#[test]
fn json_strings_escape_every_ascii_control_character() {
    let expected = [
        r"\u0000", r"\u0001", r"\u0002", r"\u0003", r"\u0004", r"\u0005", r"\u0006", r"\u0007",
        r"\b", r"\t", r"\n", r"\u000b", r"\f", r"\r", r"\u000e", r"\u000f", r"\u0010", r"\u0011",
        r"\u0012", r"\u0013", r"\u0014", r"\u0015", r"\u0016", r"\u0017", r"\u0018", r"\u0019",
        r"\u001a", r"\u001b", r"\u001c", r"\u001d", r"\u001e", r"\u001f",
    ];
    for (code, escape) in (0u8..32).zip(expected) {
        let identity = char::from(code).to_string();
        let body = diagnose_body(&identity, "motor", &[0.0], 1000.0, 25.0);
        assert!(
            body.starts_with(&format!("{{\"event_id\":\"{escape}\",")),
            "control {code}: {body:?}"
        );
        assert!(!body.bytes().any(|byte| byte < 32));
    }
}
