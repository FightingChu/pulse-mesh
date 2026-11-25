package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestSampleValueIsRequiredBeforeAnyWrites(t *testing.T) {
	const valid = `{"event_id":"valid","device_id":"motor-01","metric":"vibration_rms","value":4.8,"level":"critical"}`
	for _, invalid := range []string{
		`{"device_id":"motor-02","metric":"vibration_rms","level":"critical"}`,
		`{"device_id":"motor-02","metric":"vibration_rms","value":null,"level":"critical"}`,
		`{"device_id":"motor-02","metric":"vibration_rms","value":false,"level":"critical"}`,
		`{"device_id":"motor-02","metric":"vibration_rms","value":"0","level":"critical"}`,
	} {
		for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch"} {
			t.Run(endpoint+"/"+invalid, func(t *testing.T) {
				samples := ingest.NewStore()
				engine := alerts.New(time.Minute)
				handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10))
				body := invalid
				if endpoint == "/api/v1/samples/batch" {
					body = "[" + valid + "," + invalid + "]"
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
				if response.Code != http.StatusBadRequest {
					t.Errorf("status=%d body=%s, want 400", response.Code, response.Body.String())
				}
				if samples.Count() != 0 || len(engine.List("", true)) != 0 {
					t.Errorf("invalid input created %d samples and %d alerts", samples.Count(), len(engine.List("", true)))
				}
				var result map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["error"] == nil {
					t.Error("missing JSON validation error")
				}
			})
		}
	}
}

func TestExplicitZeroSampleValueRemainsValid(t *testing.T) {
	for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch"} {
		t.Run(endpoint, func(t *testing.T) {
			samples := ingest.NewStore()
			engine := alerts.New(time.Minute)
			handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10))
			body := `{"event_id":"zero-reading","device_id":"motor-01","metric":"vibration_rms","value":0,"temp_c":0,"level":"normal","ts":"2026-11-25T08:00:00Z"}`
			if endpoint == "/api/v1/samples/batch" {
				body = "[" + body + "]"
			}
			for attempt := 0; attempt < 2; attempt++ {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
				want := http.StatusCreated
				if attempt == 1 {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
				}
			}
			stored := samples.List("motor-01", 10)
			if len(stored) != 1 || stored[0].Value != 0 || stored[0].EventID != "zero-reading" || stored[0].Level != "normal" || stored[0].Ts.Format(time.RFC3339) != "2026-11-25T08:00:00Z" {
				t.Fatalf("stored zero reading lost fields or idempotency: %+v", stored)
			}
			if len(engine.List("", true)) != 0 {
				t.Fatal("normal zero reading generated an alert")
			}
		})
	}
}
