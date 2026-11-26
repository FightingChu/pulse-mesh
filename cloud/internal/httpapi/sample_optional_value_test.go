package httpapi_test

import (
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

func TestSamplesRejectExplicitNullOptionalNumericAndTimestampValues(t *testing.T) {
	for _, mode := range []string{"memory", "journal"} {
		for _, endpoint := range []string{"/api/v1/samples", "/api/v1/samples/batch"} {
			for _, field := range []string{"temp_c", "ts"} {
				t.Run(mode+endpoint+field, func(t *testing.T) {
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
					invalid := fmt.Sprintf(`{"event_id":"invalid","device_id":"motor-01","metric":"vibration","value":4.8,"level":"critical","%s":null}`, field)
					body := invalid
					if endpoint == "/api/v1/samples/batch" {
						body = `[ {"event_id":"valid","device_id":"pump-02","metric":"vibration","value":4.8,"level":"critical"},` + invalid + `]`
					}
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body)))
					if response.Code != http.StatusBadRequest {
						t.Fatalf("explicit null returned %d: %s", response.Code, response.Body.String())
					}
					if samples.Count() != 0 || len(engine.List("", true)) != 0 {
						t.Errorf("rejected request wrote samples=%d alerts=%d", samples.Count(), len(engine.List("", true)))
					}
					_, journalBytes, failures := samples.Stats()
					if journalBytes != 0 || failures != 0 {
						t.Errorf("rejected request changed journal: bytes=%d failures=%d", journalBytes, failures)
					}
				})
			}
		}
	}
}

func TestSamplesKeepDefaultsWhenOptionalValuesAreOmitted(t *testing.T) {
	samples := ingest.NewStore()
	handler := httpapi.New(samples, alerts.New(time.Minute), diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/samples", strings.NewReader(`{"device_id":"motor-01","metric":"vibration","value":0}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("omitted optional values returned %d: %s", response.Code, response.Body.String())
	}
	items := samples.List("", 10)
	if len(items) != 1 || items[0].TempC != 0 || items[0].Ts.IsZero() {
		t.Fatalf("omitted values did not retain defaults: %+v", items)
	}
}
