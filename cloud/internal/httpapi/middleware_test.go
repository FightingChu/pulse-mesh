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
	if !strings.Contains(text, `method="GET",path="/metrics",status="200"} 1`) {
		t.Fatalf("first scrape was not counted: %s", text)
	}
	if !strings.Contains(text, `pulse_store_records{store="samples"} 0`) ||
		!strings.Contains(text, `pulse_store_journal_bytes{store="alerts"} 0`) {
		t.Fatalf("missing fixed store metrics: %s", text)
	}

	resp, err = http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("second metrics scrape: %v", err)
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `method="GET",path="/metrics",status="200"} 2`) {
		t.Fatalf("second scrape counter is incorrect: %s", body)
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

func TestMetricsNormalizesUnknownMethodsThroughHTTP(t *testing.T) {
	server := newTestServer(t)
	defer server.Close()
	for _, path := range []string{"/api/v1/samples", "/metrics"} {
		for _, method := range []string{"CUSTOM", "get", "GET|spoof|200"} {
			request, err := http.NewRequest(method, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			wantStatus := http.StatusMethodNotAllowed
			if path == "/metrics" {
				wantStatus = http.StatusOK
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("%s %s returned %d, want %d", method, path, response.StatusCode, wantStatus)
			}
		}
	}
	response, err := server.Client().Get(server.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		`pulse_http_requests_total{method="OTHER",path="/api/v1/samples",status="405"} 3`,
		`pulse_http_requests_total{method="OTHER",path="/metrics",status="200"} 3`,
		`pulse_http_requests_total{method="GET",path="/metrics",status="200"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing metric %s in %s", want, text)
		}
	}
	if strings.Contains(text, "CUSTOM") || strings.Contains(text, "spoof") || strings.Contains(text, `method="get"`) {
		t.Errorf("untrusted method leaked into metric labels: %s", text)
	}
}
