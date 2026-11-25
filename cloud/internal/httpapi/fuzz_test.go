package httpapi

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"pulse-mesh/cloud/internal/ingest"
)

func FuzzValidateSample(f *testing.F) {
	f.Add("motor-01", "vibration_rms", "event-1", 1.25, 55.0, "normal")
	f.Add("", "", "", 0.0, 0.0, "unknown")
	f.Add("motor\x00bad", "metric", "event", 1.0, 22.0, "critical")
	f.Add("motor", string(bytes.Repeat([]byte{'x'}, 80)), "event", math.NaN(), 20.0, "warning")

	f.Fuzz(func(t *testing.T, deviceID, metric, eventID string, value, tempC float64, level string) {
		_ = validateSample(ingest.Sample{
			DeviceID: deviceID,
			Metric:   metric,
			EventID:  eventID,
			Value:    value,
			TempC:    tempC,
			Level:    level,
		})
	})
}

func FuzzDecodeJSON(f *testing.F) {
	f.Add(`{"device_id":"motor-01","metric":"vibration_rms","value":1.2}`)
	f.Add(`{"device_id":"motor-01","metric":"vibration_rms"}`)
	f.Add(`{"device_id":"motor-01","metric":"vibration_rms","value":null}`)
	f.Add(`{"device_id":"motor-01","metric":"vibration_rms","value":0}`)
	f.Add(`[]`)
	f.Add(`{"x":1} {"y":2}`)
	f.Add(`{"value":NaN}`)
	f.Add(``)

	f.Fuzz(func(t *testing.T, body string) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/samples", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		var input sampleInput
		if err := decodeJSON(recorder, request, &input); err == nil {
			_ = validateSample(input.Sample)
		}
	})
}

func FuzzSampleJSONRoundTrip(f *testing.F) {
	f.Add(`{"event_id":"e1","device_id":"motor-01","metric":"v","value":2.5,"temp_c":45,"level":"warning"}`)
	f.Add(`{"device_id":"m","metric":"x","value":1e308}`)
	f.Add(`null`)

	f.Fuzz(func(t *testing.T, body string) {
		var sample ingest.Sample
		if err := json.Unmarshal([]byte(body), &sample); err != nil {
			return
		}
		if err := validateSample(sample); err != nil {
			return
		}
		encoded, err := json.Marshal(sample)
		if err != nil {
			t.Fatalf("marshal valid sample: %v", err)
		}
		var decoded ingest.Sample
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("unmarshal marshaled sample: %v", err)
		}
		if err := validateSample(decoded); err != nil {
			t.Fatalf("round-tripped sample became invalid: %v", err)
		}
	})
}
