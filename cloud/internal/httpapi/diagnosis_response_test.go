package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestDiagnosisRejectsInvalidAIFeaturesBeforeSideEffects(t *testing.T) {
	for _, feature := range []string{"dominant_hz", "peak_amplitude", "low_band", "mid_band", "high_band"} {
		for _, invalid := range []struct {
			name  string
			value any
		}{
			{"missing", nil},
			{"null", nil},
			{"boolean", false},
			{"string", "0"},
		} {
			t.Run(feature+"/"+invalid.name, func(t *testing.T) {
				payload := map[string]any{
					"device_id": "motor-01", "event_id": "invalid-ai", "fault": "bearing_wear",
					"dominant_hz": 120.0, "peak_amplitude": 0.6,
					"low_band": 0.0, "mid_band": 0.1, "high_band": 0.9,
				}
				if invalid.name == "missing" {
					delete(payload, feature)
				} else {
					payload[feature] = invalid.value
				}
				var aiCalls atomic.Int32
				ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					aiCalls.Add(1)
					writeJSON(writer, payload)
				}))
				defer ai.Close()
				store := diagnose.NewStore(10)
				engine := alerts.New(time.Minute)
				client := diagnose.NewClientWithBreaker(ai.URL, time.Second, 1, time.Minute)
				handler := httpapi.New(ingest.NewStore(), engine, client, store)
				body, err := json.Marshal(diagnose.Request{
					DeviceID: "motor-01", EventID: "invalid-ai", Values: make([]float64, 1024),
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(body)))
					if response.Code != status {
						t.Errorf("invalid AI feature: status=%d body=%s, want %d", response.Code, response.Body.String(), status)
					}
					var result map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result["error"] == nil {
						t.Error("missing JSON upstream error")
					}
				}
				if aiCalls.Load() != 1 || store.Count() != 0 || len(engine.List("", true)) != 0 {
					t.Errorf("invalid AI response side effects: AI=%d diagnoses=%d alerts=%d", aiCalls.Load(), store.Count(), len(engine.List("", true)))
				}
			})
		}
	}
}

func TestDiagnosisRejectsOutOfRangeAIBandsBeforeSideEffects(t *testing.T) {
	for _, band := range []string{"low_band", "mid_band", "high_band"} {
		t.Run(band, func(t *testing.T) {
			payload := map[string]any{
				"device_id": "motor-01", "event_id": "invalid-band", "fault": "bearing_wear",
				"dominant_hz": 120.0, "peak_amplitude": 0.6,
				"low_band": 0.0, "mid_band": 0.0, "high_band": 0.0,
			}
			payload[band] = 1.04
			var aiCalls atomic.Int32
			ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				aiCalls.Add(1)
				writeJSON(writer, payload)
			}))
			defer ai.Close()
			store := diagnose.NewStore(10)
			engine := alerts.New(time.Minute)
			client := diagnose.NewClientWithBreaker(ai.URL, time.Second, 1, time.Minute)
			handler := httpapi.New(ingest.NewStore(), engine, client, store)
			body, err := json.Marshal(diagnose.Request{
				DeviceID: "motor-01", EventID: "invalid-band", Values: make([]float64, 1024),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(body)))
				if response.Code != status {
					t.Errorf("invalid AI band: status=%d body=%s, want %d", response.Code, response.Body.String(), status)
				}
				var result map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["error"] == nil {
					t.Error("missing JSON upstream error")
				}
			}
			if aiCalls.Load() != 1 || store.Count() != 0 || len(engine.List("", true)) != 0 {
				t.Errorf("invalid AI band caused side effects: AI=%d diagnoses=%d alerts=%d", aiCalls.Load(), store.Count(), len(engine.List("", true)))
			}
		})
	}
}

func TestDiagnosisRejectsInvalidAIUTF8BeforeSideEffects(t *testing.T) {
	for _, field := range []string{"device_id", "event_id", "unused"} {
		for _, invalid := range []string{"\xff", "\xe7", "\xc0\xaf", "\xed\xa0\x80"} {
			t.Run(fmt.Sprintf("%s/%x", field, invalid), func(t *testing.T) {
				identity := "motor-" + strings.Repeat("\ufffd", len(invalid))
				payload := map[string]any{
					"device_id": identity, "event_id": identity, "fault": "bearing_wear",
					"dominant_hz": 120, "peak_amplitude": 0.6,
					"low_band": 0, "mid_band": 0.1, "high_band": 0.9,
				}
				payload[field] = "invalid-marker"
				encoded, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				wire := strings.Replace(string(encoded), "invalid-marker", "motor-"+invalid, 1)
				var aiCalls atomic.Int32
				ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					aiCalls.Add(1)
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(wire))
				}))
				defer ai.Close()
				store := diagnose.NewStore(10)
				engine := alerts.New(time.Minute)
				client := diagnose.NewClientWithBreaker(ai.URL, time.Second, 1, time.Minute)
				handler := httpapi.New(ingest.NewStore(), engine, client, store)
				body, err := json.Marshal(diagnose.Request{
					DeviceID: identity, EventID: identity, Values: make([]float64, 1024),
				})
				if err != nil {
					t.Fatal(err)
				}
				for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(body)))
					if response.Code != status {
						t.Errorf("invalid AI encoding: status=%d body=%s, want %d", response.Code, response.Body.String(), status)
					}
					var result map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == nil {
						t.Errorf("missing JSON upstream error: %s (%v)", response.Body.String(), err)
					}
				}
				if aiCalls.Load() != 1 || store.Count() != 0 || len(engine.List("", true)) != 0 {
					t.Errorf("invalid AI encoding caused side effects: AI=%d diagnoses=%d alerts=%d", aiCalls.Load(), store.Count(), len(engine.List("", true)))
				}
			})
		}
	}
}
