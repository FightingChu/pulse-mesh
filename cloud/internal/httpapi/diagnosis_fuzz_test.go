package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func FuzzDecodeDiagnosisJSON(f *testing.F) {
	f.Add(`{"device_id":"motor-01","values":[0,-1,1.25]}`)
	f.Add(`{"device_id":"motor-01","values":[null,0]}`)
	f.Add(`{"device_id":"motor-01","values":[false]}`)
	f.Add(`{"device_id":"motor-01","values":["0"]}`)
	f.Add(`{"device_id":"motor-01","values":null}`)
	f.Add(`{"device_id":"motor-01","values":[0]} {}`)
	f.Add(`null`)

	f.Fuzz(func(t *testing.T, body string) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewBufferString(body))
		var input diagnosisInput
		if err := decodeJSON(httptest.NewRecorder(), request, &input); err != nil {
			return
		}
		var decoded struct {
			Values []json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("accepted invalid JSON: %v", err)
		}
		if len(decoded.Values) != len(input.Values) {
			t.Fatal("decoding changed the waveform length")
		}
		for index, raw := range decoded.Values {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				t.Fatalf("accepted null waveform point at %d", index)
			}
			var expected float64
			if err := json.Unmarshal(raw, &expected); err != nil {
				t.Fatalf("accepted non-numeric waveform point: %v", err)
			}
			if input.Values[index] != expected {
				t.Fatalf("changed waveform point %d", index)
			}
		}
	})
}
