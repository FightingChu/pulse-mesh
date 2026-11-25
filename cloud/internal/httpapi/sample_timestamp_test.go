package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestSampleRejectsUnencodableTimestampBeforeWriting(t *testing.T) {
	for _, mode := range []string{"memory", "journal"} {
		for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch"} {
			for _, timestamp := range []string{"2026-11-26T00:00:00+24:00", "2026-11-26T00:00:00-24:00", "2026-11-26T00:00:00+23:60"} {
				t.Run(mode+endpoint+timestamp, func(t *testing.T) {
					path := ""
					if mode == "journal" {
						path = filepath.Join(t.TempDir(), "samples.jsonl")
					}
					samples, err := ingest.OpenStore(path)
					if err != nil {
						t.Fatal(err)
					}
					defer samples.Close()
					engine := alerts.New(time.Minute)
					handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10))
					body := fmt.Sprintf(`{"event_id":"bad-time","device_id":"motor-01","metric":"vibration","value":4.8,"level":"critical","ts":%q}`, timestamp)
					if endpoint == "/api/v1/samples/batch" {
						body = `[{"event_id":"valid-time","device_id":"motor-02","metric":"vibration","value":4.8,"level":"critical"},` + body + `]`
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
					if response.Code != http.StatusBadRequest {
						t.Errorf("invalid timestamp: status=%d body=%s, want 400", response.Code, response.Body.String())
					}
					if samples.Count() != 0 || len(engine.List("", true)) != 0 {
						t.Errorf("invalid timestamp wrote samples=%d alerts=%d", samples.Count(), len(engine.List("", true)))
					}
					_, journalBytes, failures := samples.Stats()
					if journalBytes != 0 || failures != 0 {
						t.Errorf("validation changed journal: bytes=%d failures=%d", journalBytes, failures)
					}
					list := httptest.NewRecorder()
					handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/samples", nil))
					var items []ingest.Sample
					if err := json.Unmarshal(list.Body.Bytes(), &items); err != nil {
						t.Fatalf("sample list is no longer valid JSON: %v; body=%s", err, list.Body.String())
					}
					if len(items) != 0 {
						t.Errorf("invalid request left %d samples", len(items))
					}
				})
			}
		}
	}
}

func TestSamplePreservesEncodableTimestampOffsets(t *testing.T) {
	for _, timestamp := range []string{"2026-11-26T08:00:00.123456789+08:00", "2026-11-26T00:00:00-23:59", "2026-11-26T00:00:00+23:59", "2026-11-26T00:00:00Z"} {
		t.Run(timestamp, func(t *testing.T) {
			samples := ingest.NewStore()
			handler := httpapi.New(samples, alerts.New(time.Minute), diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10))
			body := fmt.Sprintf(`{"device_id":"motor-01","metric":"vibration","value":0,"ts":%q}`, timestamp)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/samples", strings.NewReader(body)))
			if response.Code != http.StatusCreated {
				t.Fatalf("valid timestamp: status=%d body=%s", response.Code, response.Body.String())
			}
			list := httptest.NewRecorder()
			handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/samples", nil))
			var items []ingest.Sample
			if err := json.Unmarshal(list.Body.Bytes(), &items); err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Ts.Format(time.RFC3339Nano) != timestamp {
				t.Fatalf("timestamp offset or precision changed: %+v", items)
			}
		})
	}
}
