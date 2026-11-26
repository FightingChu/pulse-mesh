package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestListQueriesRejectMalformedEncodingWithoutDroppingFilters(t *testing.T) {
	samples := ingest.NewStore()
	engine := alerts.New(time.Minute)
	diagnoses := diagnose.NewStore(10)
	const device = "电机+east;1"
	for _, identity := range []string{device, "motor-02"} {
		sample := ingest.Sample{DeviceID: identity, Metric: "vibration", Value: 4.8, Level: "critical"}
		if added, err := samples.Add(sample); err != nil || !added {
			t.Fatalf("seed sample = %v, %v", added, err)
		}
		if created, err := engine.EvaluateWithError(sample); err != nil || created == nil {
			t.Fatalf("seed alert = %+v, %v", created, err)
		}
		if added, err := diagnoses.Add(diagnose.Result{DeviceID: identity, Fault: "bearing_wear"}); err != nil || !added {
			t.Fatalf("seed diagnosis = %v, %v", added, err)
		}
	}
	handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnoses)
	for _, endpoint := range []string{"/api/v1/samples", "/api/v1/alerts", "/api/v1/diagnoses"} {
		for _, query := range []string{
			"device=%zz", "device=%", "device=motor-02;limit=1",
			"device=motor-02&limit=%zz",
			"device=motor-02&%zz=ignored",
		} {
			t.Run(endpoint+"?"+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?"+query, nil))
				if response.Code != http.StatusBadRequest {
					t.Errorf("malformed query returned %d: %s", response.Code, response.Body.String())
				}
				var result map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == "" {
					t.Errorf("expected JSON error, got %s (%v)", response.Body.String(), err)
				}
			})
		}
		for _, query := range []string{"", "device=" + url.QueryEscape(device) + "&limit=1&include_acked=1"} {
			t.Run(endpoint+"/valid/"+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?"+query, nil))
				if response.Code != http.StatusOK {
					t.Fatalf("valid query returned %d: %s", response.Code, response.Body.String())
				}
				var items []struct {
					DeviceID string `json:"device_id"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
					t.Fatal(err)
				}
				if query == "" {
					if len(items) != 2 {
						t.Errorf("unfiltered list has %d items, want 2", len(items))
					}
				} else if len(items) != 1 || items[0].DeviceID != device {
					t.Errorf("encoded device filter lost identity: %+v", items)
				}
			})
		}
	}
	for _, endpoint := range []string{"/api/v1/samples", "/api/v1/diagnoses"} {
		for _, query := range []string{"limit=1&limit=100", "limit=100&limit=1"} {
			t.Run(endpoint+"?"+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?"+query, nil))
				if response.Code != http.StatusBadRequest {
					t.Errorf("duplicate limit returned %d: %s", response.Code, response.Body.String())
				}
			})
		}
	}
	for _, query := range []string{"include_acked=0&include_acked=1", "include_acked=1&include_acked=0"} {
		t.Run("/api/v1/alerts?"+query, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/alerts?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Errorf("duplicate include_acked returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestListQueriesRejectDuplicateRecognizedParameters(t *testing.T) {
	samples := ingest.NewStore()
	engine := alerts.New(time.Minute)
	diagnoses := diagnose.NewStore(10)
	for _, device := range []string{"motor-01", "pump-02"} {
		sample := ingest.Sample{DeviceID: device, Metric: "vibration", Value: 1.2}
		if added, err := samples.Add(sample); err != nil || !added {
			t.Fatalf("seed sample: %v %v", added, err)
		}
		if added, err := diagnoses.Add(diagnose.Result{DeviceID: device, Fault: "normal"}); err != nil || !added {
			t.Fatalf("seed diagnosis: %v %v", added, err)
		}
	}
	handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnoses)
	for _, request := range []struct{ endpoint, query string }{
		{"/api/v1/samples", "device=motor-01&device=pump-02"},
		{"/api/v1/samples", "device=&device=pump-02"},
		{"/api/v1/samples", "limit=1&limit=2"},
		{"/api/v1/diagnoses", "device=motor-01&device=pump-02"},
		{"/api/v1/diagnoses", "limit=1&limit=2"},
		{"/api/v1/alerts", "device=motor-01&device=pump-02"},
		{"/api/v1/alerts", "include_acked=0&include_acked=1"},
	} {
		t.Run(request.endpoint+"?"+request.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, request.endpoint+"?"+request.query, nil))
			if response.Code != http.StatusBadRequest {
				t.Errorf("duplicate query parameter returned %d: %s", response.Code, response.Body.String())
			}
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == "" {
				t.Errorf("expected JSON error, got %s (%v)", response.Body.String(), err)
			}
		})
	}
}
