package httpapi_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/httpapi"
	"pulse-mesh/cloud/internal/ingest"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req diagnose.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, diagnose.Result{
			DeviceID:   req.DeviceID,
			DominantHz: 120,
			HighBand:   0.9,
			Fault:      "bearing_wear",
		})
	}))
	t.Cleanup(ai.Close)
	return httptest.NewServer(httpapi.New(
		ingest.NewStore(),
		alerts.New(time.Minute),
		diagnose.NewClient(ai.URL),
		diagnose.NewStore(10),
	))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
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
	srv := newTestServer(t)
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
	srv := newTestServer(t)
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

func TestPostRejectsInvalidSampleFields(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	cases := []string{
		`{"device_id":"motor-01","metric":"vibration_rms","value":1,"level":"urgent"}`,
		`{"device_id":"motor-01","metric":"vibration_rms","value":NaN}`,
		`{"device_id":"motor-01","metric":"vibration_rms","value":1} {}`,
	}
	for _, payload := range cases {
		resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("payload %q status = %d, want 400", payload, resp.StatusCode)
		}
	}
}

func TestPostRejectsOversizedBody(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload := `{"device_id":"motor-01","metric":"vibration_rms","padding":"` + strings.Repeat("x", 128*1024) + `"}`
	resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestRejectsUnknownMethod(t *testing.T) {
	srv := newTestServer(t)
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
	srv := newTestServer(t)
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

func TestDiagnoseStoresResultAndRaisesAlert(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	values := make([]float64, 1024)
	payload, err := json.Marshal(diagnose.Request{
		DeviceID:     "motor-01",
		Values:       values,
		SampleRateHz: 1000,
		RotationHz:   25,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Diagnosis diagnose.Result `json:"diagnosis"`
		Alert     *alerts.Alert   `json:"alert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Diagnosis.Fault != "bearing_wear" {
		t.Fatalf("fault = %q, want bearing_wear", body.Diagnosis.Fault)
	}
	if body.Alert == nil || body.Alert.Level != "critical" {
		t.Fatalf("expected critical alert, got %+v", body.Alert)
	}
	if body.Alert.Metric != "fault:bearing_wear" {
		t.Fatalf("metric = %q", body.Alert.Metric)
	}

	resp, err = http.Get(srv.URL + "/api/v1/diagnoses?device=motor-01")
	if err != nil {
		t.Fatalf("list diagnoses: %v", err)
	}
	defer resp.Body.Close()
	var stored []diagnose.Result
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatalf("decode diagnoses: %v", err)
	}
	if len(stored) != 1 || stored[0].Fault != "bearing_wear" {
		t.Fatalf("unexpected stored diagnoses: %+v", stored)
	}
}

func TestDiagnoseRejectsBadPayload(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(`{"device_id":"motor-01"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDiagnoseRejectsWrongWaveformLength(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload, _ := json.Marshal(diagnose.Request{DeviceID: "motor-01", Values: make([]float64, 1023)})
	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDiagnoseRejectsNonFiniteWaveform(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	values := make([]float64, 1024)
	values[17] = math.Inf(1)
	payload, _ := json.Marshal(diagnose.Request{DeviceID: "motor-01", Values: values})
	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestDiagnoseReturnsBadGatewayWhenAIDown(t *testing.T) {
	deadAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadAI.URL
	deadAI.Close()

	srv := httptest.NewServer(httpapi.New(
		ingest.NewStore(),
		alerts.New(time.Minute),
		diagnose.NewClient(deadURL),
		diagnose.NewStore(10),
	))
	defer srv.Close()

	values := make([]float64, 1024)
	payload, _ := json.Marshal(diagnose.Request{DeviceID: "motor-01", Values: values})
	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}
