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

func TestJSONRequestsRejectInvalidUTF8BeforeSideEffects(t *testing.T) {
	for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch", "/api/v1/diagnose"} {
		for _, field := range []string{"device_id", "event_id", "unused"} {
			for _, invalid := range []string{"\xff", "\xe7", "\xc0\xaf", "\xed\xa0\x80"} {
				t.Run(fmt.Sprintf("%s/%s/%x", endpoint, field, invalid), func(t *testing.T) {
					payload := map[string]any{
						"device_id": "motor-01", "event_id": "event-01", "metric": "vibration",
						"value": 4.8, "level": "critical", "values": make([]float64, 1024),
					}
					payload[field] = "invalid-marker"
					encoded, err := json.Marshal(payload)
					if err != nil {
						t.Fatal(err)
					}
					body := strings.Replace(string(encoded), "invalid-marker", invalid, 1)
					if endpoint == "/api/v1/samples/batch" {
						body = `[{"device_id":"valid","metric":"vibration","value":4.8,"level":"critical"},` + body + `]`
					}
					var aiCalls atomic.Int32
					ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
						aiCalls.Add(1)
						var input diagnose.Request
						if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
							t.Error(err)
							return
						}
						writeJSON(writer, diagnose.Result{DeviceID: input.DeviceID, EventID: input.EventID, Fault: "bearing_wear"})
					}))
					defer ai.Close()
					samples := ingest.NewStore()
					diagnoses := diagnose.NewStore(10)
					engine := alerts.New(time.Minute)
					handler := httpapi.New(samples, engine, diagnose.NewClient(ai.URL), diagnoses)
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
					if response.Code != http.StatusBadRequest {
						t.Errorf("invalid UTF-8 returned %d, want 400", response.Code)
					}
					var result map[string]any
					if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == nil {
						t.Errorf("missing JSON error: %s (%v)", response.Body.String(), err)
					}
					if samples.Count() != 0 || diagnoses.Count() != 0 || len(engine.List("", true)) != 0 || aiCalls.Load() != 0 {
						t.Errorf("invalid UTF-8 caused writes or analysis: samples=%d diagnoses=%d alerts=%d AI=%d", samples.Count(), diagnoses.Count(), len(engine.List("", true)), aiCalls.Load())
					}
				})
			}
		}
	}
}

func TestJSONRequestsPreserveUnicodeAndBodyLimit(t *testing.T) {
	const device = "电机-\ufffd-😀"
	const limit = 128 * 1024
	for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch", "/api/v1/diagnose"} {
		for _, size := range []int{0, limit, limit + 1} {
			t.Run(fmt.Sprintf("%s/size=%d", endpoint, size), func(t *testing.T) {
				body, err := json.Marshal(map[string]any{
					"device_id": device, "metric": "vibration", "value": 0, "values": make([]float64, 1024),
				})
				if err != nil {
					t.Fatal(err)
				}
				if endpoint == "/api/v1/samples/batch" {
					body = append(append([]byte{'['}, body...), ']')
				}
				if size > 0 {
					body = append(body, bytes.Repeat([]byte{' '}, size-len(body))...)
				}
				var aiCalls atomic.Int32
				ai := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					aiCalls.Add(1)
					var input diagnose.Request
					if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
						t.Error(err)
						return
					}
					if input.DeviceID != device {
						t.Errorf("forwarded Unicode identity changed: %q", input.DeviceID)
					}
					writeJSON(writer, diagnose.Result{DeviceID: input.DeviceID, Fault: "normal"})
				}))
				defer ai.Close()
				samples := ingest.NewStore()
				diagnoses := diagnose.NewStore(10)
				engine := alerts.New(time.Minute)
				handler := httpapi.New(samples, engine, diagnose.NewClient(ai.URL), diagnoses)
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body)))
				if size > limit {
					if response.Code != http.StatusRequestEntityTooLarge || samples.Count() != 0 || diagnoses.Count() != 0 || aiCalls.Load() != 0 {
						t.Fatalf("oversized request: status=%d samples=%d diagnoses=%d AI=%d", response.Code, samples.Count(), diagnoses.Count(), aiCalls.Load())
					}
				} else if endpoint == "/api/v1/diagnose" {
					items := diagnoses.List(device, 10)
					if response.Code != http.StatusOK || len(items) != 1 || items[0].DeviceID != device || aiCalls.Load() != 1 {
						t.Fatalf("valid Unicode diagnosis: status=%d items=%+v AI=%d", response.Code, items, aiCalls.Load())
					}
				} else {
					items := samples.List(device, 10)
					if response.Code != http.StatusCreated || len(items) != 1 || items[0].DeviceID != device {
						t.Fatalf("valid Unicode sample: status=%d items=%+v", response.Code, items)
					}
				}
			})
		}
	}
}
