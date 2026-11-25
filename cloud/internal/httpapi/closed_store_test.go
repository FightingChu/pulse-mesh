package httpapi_test

import (
	"encoding/json"
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

func TestClosedDiagnosisStoreRejectsBeforeAI(t *testing.T) {
	var calls atomic.Int32
	aiServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer aiServer.Close()
	store := diagnose.NewStore(10)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(ingest.NewStore(), alerts.New(time.Minute), diagnose.NewClient(aiServer.URL), store)
	payload, err := json.Marshal(diagnose.Request{DeviceID: "motor-01", Values: make([]float64, 1024)})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(string(payload))))
	if recorder.Code != http.StatusServiceUnavailable || calls.Load() != 0 {
		t.Fatalf("closed diagnosis: status=%d AI calls=%d", recorder.Code, calls.Load())
	}
}

func TestReadinessIdentifiesEachClosedStoreWhileLivenessStaysUp(t *testing.T) {
	for _, storeName := range []string{"samples", "diagnoses", "alerts"} {
		t.Run(storeName, func(t *testing.T) {
			samples := ingest.NewStore()
			diagnoses := diagnose.NewStore(10)
			engine := alerts.New(time.Minute)
			var closeErr error
			switch storeName {
			case "samples":
				closeErr = samples.Close()
			case "diagnoses":
				closeErr = diagnoses.Close()
			case "alerts":
				closeErr = engine.Close()
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnoses)
			ready := httptest.NewRecorder()
			handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			var body map[string]string
			if err := json.Unmarshal(ready.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if ready.Code != http.StatusServiceUnavailable || body["store"] != storeName || body["status"] != "not_ready" {
				t.Fatalf("readiness: status=%d body=%v", ready.Code, body)
			}
			live := httptest.NewRecorder()
			handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
			if live.Code != http.StatusOK {
				t.Fatalf("liveness status=%d", live.Code)
			}
		})
	}
}
