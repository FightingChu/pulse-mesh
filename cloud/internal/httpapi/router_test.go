package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func newTestServer() *httptest.Server {
	return httptest.NewServer(httpapi.New(ingest.NewStore(), alerts.New(time.Minute)))
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

func TestCriticalSampleCreatesAlertAndAck(t *testing.T) {
	srv := newTestServer()
	defer srv.Close()

	payload := `{"device_id":"motor-01","metric":"vibration_rms","value":4.8,"level":"critical"}`
	resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	var created struct {
		Alert *alerts.Alert `json:"alert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Alert == nil || created.Alert.Level != "critical" {
		t.Fatalf("expected critical alert, got %+v", created.Alert)
	}

	resp, err = http.Get(srv.URL + "/api/v1/alerts?device=motor-01")
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	defer resp.Body.Close()
	var list []alerts.Alert
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(list))
	}

	resp, err = http.Post(srv.URL+"/api/v1/alerts/"+created.Alert.ID+"/ack", "application/json", nil)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ack status = %d, want 200", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/api/v1/alerts?device=motor-01")
	if err != nil {
		t.Fatalf("list alerts after ack: %v", err)
	}
	defer resp.Body.Close()
	var after []alerts.Alert
	if err := json.NewDecoder(resp.Body).Decode(&after); err != nil {
		t.Fatalf("decode alerts after ack: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("acked alert should be hidden, got %d", len(after))
	}

	resp, err = http.Get(srv.URL + "/api/v1/alerts?device=motor-01&include_acked=1")
	if err != nil {
		t.Fatalf("list alerts with acked: %v", err)
	}
	defer resp.Body.Close()
	var withAcked []alerts.Alert
	if err := json.NewDecoder(resp.Body).Decode(&withAcked); err != nil {
		t.Fatalf("decode acked: %v", err)
	}
	if len(withAcked) != 1 || !withAcked[0].Acked {
		t.Fatalf("expected one acked alert, got %+v", withAcked)
	}
}
