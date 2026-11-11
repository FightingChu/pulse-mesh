package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func newTestServer() *httptest.Server {
	return httptest.NewServer(httpapi.New(ingest.NewStore()))
}

func TestHealth(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want ok", body["status"])
	}
}

func TestPostThenList(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	payload := `{"device_id":"motor-01","metric":"vibration_rms","value":4.8}`
	resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp, err = http.Get(srv.URL + "/api/v1/samples?device=motor-01")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer resp.Body.Close()

	var samples []ingest.Sample
	if err := json.NewDecoder(resp.Body).Decode(&samples); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("len(samples) = %d, want 1", len(samples))
	}
	if samples[0].DeviceID != "motor-01" || samples[0].Value != 4.8 {
		t.Fatalf("unexpected sample: %+v", samples[0])
	}
}

func TestPostRejectsMissingDevice(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(`{"value":1}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestRejectsUnknownMethod(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/samples", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}
