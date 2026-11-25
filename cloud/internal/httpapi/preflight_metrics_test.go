package httpapi_test

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func TestPreflightIsLoggedAndCountedWithoutBusinessSideEffects(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	samples := ingest.NewStore()
	engine := alerts.New(time.Minute)
	diagnoses := diagnose.NewStore(10)
	handler := httpapi.New(samples, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnoses)
	paths := []struct {
		path  string
		label string
	}{
		{"/health", "/health"},
		{"/api/v1/samples", "/api/v1/samples"},
		{"/api/v1/samples/batch", "/api/v1/samples/batch"},
		{"/api/v1/diagnose", "/api/v1/diagnose"},
		{"/api/v1/alerts/alert-1/ack", "/api/v1/alerts/{id}/ack"},
		{"/metrics", "/metrics"},
		{"/unknown", "unmatched"},
	}
	for _, target := range paths {
		for attempt := 0; attempt < 2; attempt++ {
			output.Reset()
			request := httptest.NewRequest(http.MethodOptions, target.path, strings.NewReader("invalid JSON"))
			request.Header.Set("Origin", "https://dashboard.example")
			request.Header.Set("Access-Control-Request-Method", "POST")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Errorf("preflight %s: status=%d body=%s", target.path, response.Code, response.Body.String())
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "*" ||
				response.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" ||
				response.Header().Get("Access-Control-Allow-Headers") != "Content-Type" {
				t.Errorf("preflight lost CORS headers: %v", response.Header())
			}
			want := `"OPTIONS" ` + strconv.Quote(target.path) + " 204 "
			if strings.Count(output.String(), "\n") != 1 || !strings.Contains(output.String(), want) {
				t.Errorf("preflight %s not logged exactly once: %q", target.path, output.String())
			}
		}
	}
	if samples.Count() != 0 || diagnoses.Count() != 0 || len(engine.List("", true)) != 0 {
		t.Fatal("preflight modified business data")
	}
	for scrape := 1; scrape <= 2; scrape++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		for _, target := range paths {
			want := fmt.Sprintf("pulse_http_requests_total{method=\"OPTIONS\",path=%q,status=\"204\"} 2\n", target.label)
			if !strings.Contains(response.Body.String(), want) {
				t.Errorf("missing preflight counter %s", want)
			}
		}
		want := fmt.Sprintf("pulse_http_requests_total{method=\"GET\",path=\"/metrics\",status=\"200\"} %d\n", scrape)
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("scrape counted incorrectly: %s", response.Body.String())
		}
		if strings.Contains(response.Body.String(), `method="OPTIONS",path="/metrics",status="200"`) {
			t.Error("metrics preflight was counted as a successful scrape")
		}
	}
}
