package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCORSPreflight(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/samples", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow-origin = %q, want *", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Fatalf("allow-methods = %q", got)
	}
}

func TestMetricsCountsRequests(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL + "/health")
		if err != nil {
			t.Fatalf("get health: %v", err)
		}
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "# TYPE pulse_http_requests_total counter") {
		t.Fatalf("missing metrics header: %s", text)
	}
	if !strings.Contains(text, `path="/health"`) || !strings.Contains(text, `status="200"`) {
		t.Fatalf("missing health counter: %s", text)
	}
}

func TestMetricsNormalizesAckPath(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/alerts/alert-99/ack", "application/json", nil)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "/api/v1/alerts/{id}/ack") {
		t.Fatalf("ack path was not normalized: %s", body)
	}
}
