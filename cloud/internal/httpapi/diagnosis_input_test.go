package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestDiagnosisRejectsNullPointsBeforeSideEffects(t *testing.T) {
	for _, cached := range []bool{false, true} {
		for _, index := range []int{0, 512, 1023} {
			t.Run(fmt.Sprintf("cached=%v/index=%d", cached, index), func(t *testing.T) {
				var aiCalls atomic.Int32
				ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					aiCalls.Add(1)
					writeJSON(writer, diagnose.Result{
						DeviceID: "motor-01", EventID: "null-point", Fault: "bearing_wear",
					})
				}))
				defer ai.Close()
				store := diagnose.NewStore(10)
				engine := alerts.New(time.Minute)
				initialCount := 0
				if cached {
					added, err := store.Add(diagnose.Result{
						DeviceID: "motor-01", EventID: "null-point", Fault: "bearing_wear",
					})
					if !added || err != nil {
						t.Fatalf("seed cached diagnosis = %v, %v", added, err)
					}
					initialCount = 1
				}
				handler := httpapi.New(ingest.NewStore(), engine, diagnose.NewClient(ai.URL), store)
				values := make([]any, 1024)
				for position := range values {
					values[position] = 0.0
				}
				values[index] = nil
				body, err := json.Marshal(map[string]any{
					"device_id": "motor-01", "event_id": "null-point", "values": values,
				})
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(body)))
				if response.Code != http.StatusBadRequest {
					t.Errorf("null waveform: status=%d body=%s, want 400", response.Code, response.Body.String())
				}
				if aiCalls.Load() != 0 || store.Count() != initialCount || len(engine.List("", true)) != 0 {
					t.Errorf("invalid waveform caused side effects: AI=%d diagnoses=%d alerts=%d", aiCalls.Load(), store.Count(), len(engine.List("", true)))
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

func TestDiagnosisPreservesNumericWaveformAndDefaults(t *testing.T) {
	for _, explicitFrequencies := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_frequencies=%v", explicitFrequencies), func(t *testing.T) {
			values := make([]float64, 1024)
			if explicitFrequencies {
				for index := range values {
					values[index] = float64(index%7-3) / 2
				}
			}
			want := diagnose.Request{
				DeviceID: "motor-01", EventID: "valid-waveform", Values: values,
				SampleRateHz: 1000, RotationHz: 25,
			}
			body := map[string]any{"device_id": want.DeviceID, "event_id": want.EventID, "values": values}
			if explicitFrequencies {
				want.SampleRateHz = 2048
				want.RotationHz = 30
				body["sample_rate_hz"] = want.SampleRateHz
				body["rotation_hz"] = want.RotationHz
			}
			var aiCalls atomic.Int32
			ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				aiCalls.Add(1)
				var actual diagnose.Request
				if err := json.NewDecoder(request.Body).Decode(&actual); err != nil {
					t.Errorf("decode forwarded waveform: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				if !reflect.DeepEqual(actual, want) {
					t.Error("forwarded request lost waveform values, identity or frequency parameters")
				}
				writeJSON(writer, diagnose.Result{DeviceID: actual.DeviceID, EventID: actual.EventID, Fault: "normal"})
			}))
			defer ai.Close()
			store := diagnose.NewStore(10)
			engine := alerts.New(time.Minute)
			handler := httpapi.New(ingest.NewStore(), engine, diagnose.NewClient(ai.URL), store)
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(payload)))
				if response.Code != http.StatusOK {
					t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
				}
			}
			if aiCalls.Load() != 1 || store.Count() != 1 || len(engine.List("", true)) != 0 {
				t.Fatalf("numeric waveform idempotency: AI=%d diagnoses=%d alerts=%d", aiCalls.Load(), store.Count(), len(engine.List("", true)))
			}
		})
	}
}

func TestDiagnosisPreservesExplicitZeroRotation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		rotation json.RawMessage
		want     float64
	}{
		{"omitted", nil, 25},
		{"null", json.RawMessage(`null`), 25},
		{"zero", json.RawMessage(`0`), 0},
		{"decimal zero", json.RawMessage(`0.0`), 0},
		{"negative zero", json.RawMessage(`-0`), 0},
		{"positive", json.RawMessage(`30`), 30},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var aiCalls atomic.Int32
			ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				aiCalls.Add(1)
				var actual diagnose.Request
				if err := json.NewDecoder(request.Body).Decode(&actual); err != nil {
					t.Errorf("decode forwarded request: %v", err)
					writer.WriteHeader(http.StatusBadRequest)
					return
				}
				if actual.RotationHz != scenario.want || actual.SampleRateHz != 1000 {
					t.Errorf("forwarded rotation=%v rate=%v, want rotation=%v rate=1000", actual.RotationHz, actual.SampleRateHz, scenario.want)
				}
				fault := "normal"
				if actual.RotationHz > 0 {
					fault = "imbalance"
				}
				writeJSON(writer, diagnose.Result{DeviceID: actual.DeviceID, EventID: actual.EventID, Fault: fault})
			}))
			defer ai.Close()
			store := diagnose.NewStore(10)
			engine := alerts.New(time.Minute)
			handler := httpapi.New(ingest.NewStore(), engine, diagnose.NewClient(ai.URL), store)
			body := map[string]any{"device_id": "motor-01", "event_id": "zero-rotation", "values": make([]float64, 1024)}
			if scenario.rotation != nil {
				body["rotation_hz"] = scenario.rotation
			}
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", bytes.NewReader(payload)))
				if response.Code != http.StatusOK {
					t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
				}
			}
			if aiCalls.Load() != 1 || store.Count() != 1 {
				t.Fatalf("retry did not deduplicate: AI=%d diagnoses=%d", aiCalls.Load(), store.Count())
			}
			if scenario.want == 0 {
				if store.List("motor-01", 1)[0].Fault != "normal" || len(engine.List("", true)) != 0 {
					t.Error("substituting a non-zero rotation changed the diagnosis or raised an alert")
				}
			} else if len(engine.List("", true)) != 1 {
				t.Error("positive rotation response should retain its alert behavior")
			}
		})
	}
}
