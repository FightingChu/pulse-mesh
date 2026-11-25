package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsBoundsUnknownMethodLabels(t *testing.T) {
	metrics := NewMetrics()
	const requests = 512
	for index := range requests {
		metrics.observe(fmt.Sprintf("CUSTOM-%d", index), "/api/v1/samples", http.StatusMethodNotAllowed)
	}
	if len(metrics.counts) != 1 {
		t.Fatalf("unknown methods created %d metric keys, want 1", len(metrics.counts))
	}
	if got := metrics.counts["OTHER|/api/v1/samples|405"]; got != requests {
		t.Fatalf("aggregated unknown-method count = %d, want %d", got, requests)
	}
}

func TestMetricsPreservesStandardMethodLabels(t *testing.T) {
	metrics := NewMetrics()
	methods := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodConnect,
		http.MethodOptions, http.MethodTrace,
	}
	for _, method := range methods {
		metrics.observe(method, "/health", http.StatusOK)
	}
	if len(metrics.counts) != len(methods) {
		t.Fatalf("standard method keys = %d, want %d", len(metrics.counts), len(methods))
	}
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, method := range methods {
		want := fmt.Sprintf(`pulse_http_requests_total{method=%q,path="/health",status="200"} 1`, method)
		if !strings.Contains(response.Body.String(), want) {
			t.Errorf("missing standard method counter: %s", want)
		}
	}
}
