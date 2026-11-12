package httpapi_test

import (
	"encoding/json"
	"fmt"
	"math"
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

func newTestServer(t *testing.T) *httptest.Server {
	return newTestServerWithAI(t, nil)
}

func newTestServerWithAI(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		var req diagnose.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, diagnose.Result{
			EventID:    req.EventID,
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

func TestHealthReadinessAndLiveness(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	for path, want := range map[string]int{
		"/health/live":  http.StatusOK,
		"/health/ready": http.StatusOK,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s status = %d, want %d", path, resp.StatusCode, want)
		}
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

func TestSamplesListHonorsBoundedLimit(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	for index := 0; index < 3; index++ {
		payload := fmt.Sprintf(`{"device_id":"motor-01","metric":"vibration","value":%d}`, index)
		resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	resp, err := http.Get(srv.URL + "/api/v1/samples?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var samples []ingest.Sample
	if err := json.NewDecoder(resp.Body).Decode(&samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || samples[0].Value != 2 || samples[1].Value != 1 {
		t.Fatalf("samples = %+v, want newest two", samples)
	}
}

func TestListRejectsInvalidLimit(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	for _, limit := range []string{"0", "-1", "101", "many"} {
		resp, err := http.Get(srv.URL + "/api/v1/samples?limit=" + limit)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("limit %q status = %d, want 400", limit, resp.StatusCode)
		}
	}
}

func TestDuplicateSampleEventIsNotStoredTwice(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload := `{"event_id":"boot-1-sample-0","device_id":"motor-01","metric":"vibration_rms","value":4.8,"level":"critical"}`
	post := func() map[string]any {
		resp, err := http.Post(srv.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}
	first := post()
	second := post()
	if first["duplicate"] == true || second["duplicate"] != true {
		t.Fatalf("unexpected idempotency responses: first=%v second=%v", first, second)
	}
	if first["stored"] != float64(1) || second["stored"] != float64(1) {
		t.Fatalf("sample count changed on retry: first=%v second=%v", first["stored"], second["stored"])
	}
}

func TestDuplicateSampleEventDoesNotRaiseAlertAgain(t *testing.T) {
	store := ingest.NewStore()
	engine := alerts.New(time.Nanosecond)
	server := httptest.NewServer(httpapi.New(store, engine, diagnose.NewClient("http://127.0.0.1:1"), diagnose.NewStore(10)))
	defer server.Close()

	payload := `{"event_id":"boot-1-sample-42","device_id":"motor-01","metric":"vibration_rms","value":4.8,"level":"critical"}`
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := http.Post(server.URL+"/api/v1/samples", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if attempt == 0 && resp.StatusCode != http.StatusCreated {
			t.Fatalf("first status = %d", resp.StatusCode)
		}
		time.Sleep(time.Millisecond)
	}
	if got := len(engine.List("", true)); got != 1 {
		t.Fatalf("alerts = %d, want 1 after event retry", got)
	}
}

func TestBatchSamplesValidatesThenStoresAndDeduplicates(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload := `[
		{"event_id":"batch-1","device_id":"motor-01","metric":"vibration_rms","value":1.2,"level":"normal"},
		{"event_id":"batch-2","device_id":"motor-01","metric":"vibration_rms","value":4.8,"level":"critical"}
	]`
	post := func(body string) (int, map[string]any) {
		resp, err := http.Post(srv.URL+"/api/v1/samples/batch", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post batch: %v", err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode batch: %v", err)
		}
		return resp.StatusCode, result
	}
	status, first := post(payload)
	if status != http.StatusCreated || first["stored"] != float64(2) || first["duplicates"] != float64(0) {
		t.Fatalf("first response: status=%d body=%v", status, first)
	}
	status, retry := post(payload)
	if status != http.StatusOK || retry["stored"] != float64(0) || retry["duplicates"] != float64(2) {
		t.Fatalf("retry response: status=%d body=%v", status, retry)
	}
	if got := len(retry["alerts"].([]any)); got != 0 {
		t.Fatalf("retry created %d alerts", got)
	}
	resp, err := http.Get(srv.URL + "/api/v1/samples?device=motor-01")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stored []ingest.Sample
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored samples = %d, want 2", len(stored))
	}
}

func TestBatchRejectsInvalidItemBeforeWritingAny(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload := `[{"event_id":"batch-1","device_id":"motor-01","metric":"v","value":1},{"event_id":"batch-2","device_id":"","metric":"v","value":2}]`
	resp, err := http.Post(srv.URL+"/api/v1/samples/batch", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/v1/samples?device=motor-01")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stored []ingest.Sample
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("partial batch wrote %d samples", len(stored))
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
		`{"device_id":"motor-01","metric":"vibration_rms","event_id":"` + strings.Repeat("x", 129) + `","value":1}`,
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

func TestBatchRejectsControlCharactersBeforeWriting(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	payload := `[{"event_id":"batch-1","device_id":"motor-01","metric":"v","value":1},{"event_id":"batch-2","device_id":"motor-02\u0000bad","metric":"v","value":2}]`
	resp, err := http.Post(srv.URL+"/api/v1/samples/batch", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/v1/samples?device=motor-01")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stored []ingest.Sample
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("invalid batch partially wrote %d records", len(stored))
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
	var aiCalls atomic.Int32
	srv := newTestServerWithAI(t, &aiCalls)
	defer srv.Close()

	values := make([]float64, 1024)
	payload, err := json.Marshal(diagnose.Request{
		EventID:      "boot-1-spectrum-0",
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

	resp, err = http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("retry diagnose: %v", err)
	}
	defer resp.Body.Close()
	var duplicate struct {
		Duplicate bool            `json:"duplicate"`
		Diagnosis diagnose.Result `json:"diagnosis"`
		Alert     *alerts.Alert   `json:"alert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&duplicate); err != nil {
		t.Fatalf("decode retry: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Diagnosis.EventID != "boot-1-spectrum-0" || duplicate.Alert != nil {
		t.Fatalf("unexpected duplicate response: %+v", duplicate)
	}
	if got := aiCalls.Load(); got != 1 {
		t.Fatalf("AI calls = %d, want 1 after retry", got)
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

func TestDiagnoseDefaultsMissingFrequencies(t *testing.T) {
	var aiCalls atomic.Int32
	srv := newTestServerWithAI(t, &aiCalls)
	defer srv.Close()
	values := make([]float64, 1024)
	payload := map[string]any{"device_id": "motor-01", "values": values}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if aiCalls.Load() != 1 {
		t.Fatalf("AI calls = %d, want 1", aiCalls.Load())
	}
}

func TestDiagnoseRejectsLowSampleRateEvenForDuplicateID(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	values := make([]float64, 1024)
	request := diagnose.Request{EventID: "duplicate-id", DeviceID: "motor-01", Values: values, SampleRateHz: 100, RotationHz: 25}
	body, _ := json.Marshal(request)
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("attempt %d status = %d, want 400", attempt, resp.StatusCode)
		}
	}
}

func TestDiagnoseRejectsInvalidIdentityBeforeCallingAI(t *testing.T) {
	var aiCalls atomic.Int32
	srv := newTestServerWithAI(t, &aiCalls)
	defer srv.Close()
	values := make([]float64, 1024)
	cases := []diagnose.Request{
		{DeviceID: strings.Repeat("d", 65), Values: values, SampleRateHz: 1000, RotationHz: 25},
		{DeviceID: "motor-01", EventID: strings.Repeat("e", 129), Values: values, SampleRateHz: 1000, RotationHz: 25},
		{DeviceID: "motor-01\x00bad", Values: values, SampleRateHz: 1000, RotationHz: 25},
	}
	for _, request := range cases {
		payload, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	}
	if calls := aiCalls.Load(); calls != 0 {
		t.Fatalf("AI calls = %d, want 0", calls)
	}
}

func TestDiagnosesListHonorsLimit(t *testing.T) {
	store := diagnose.NewStore(10)
	for index := 0; index < 3; index++ {
		added, err := store.Add(diagnose.Result{DeviceID: "motor-01", EventID: fmt.Sprintf("event-%d", index), Fault: "normal"})
		if err != nil || !added {
			t.Fatalf("add result %d = %v, %v", index, added, err)
		}
	}
	srv := httptest.NewServer(httpapi.New(ingest.NewStore(), alerts.New(time.Minute), diagnose.NewClient("http://127.0.0.1:1"), store))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/diagnoses?device=motor-01&limit=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var results []diagnose.Result
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].EventID != "event-2" || results[1].EventID != "event-1" {
		t.Fatalf("results = %+v, want newest two", results)
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

func TestDiagnoseConcurrencyLimit(t *testing.T) {
	const concurrencyLimit = 4
	started := make(chan struct{}, concurrencyLimit)
	release := make(chan struct{})
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		_ = json.NewEncoder(w).Encode(diagnose.Result{DeviceID: "motor-01", Fault: "normal"})
	}))
	defer ai.Close()
	srv := httptest.NewServer(httpapi.New(
		ingest.NewStore(),
		alerts.New(time.Minute),
		diagnose.NewClient(ai.URL),
		diagnose.NewStore(10),
	))
	defer srv.Close()

	values := make([]float64, 1024)
	payload, _ := json.Marshal(diagnose.Request{DeviceID: "motor-01", Values: values})
	results := make(chan int, concurrencyLimit)
	for i := 0; i < concurrencyLimit; i++ {
		go func() {
			resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
			if err != nil {
				results <- 0
				return
			}
			resp.Body.Close()
			results <- resp.StatusCode
		}()
	}
	for i := 0; i < concurrencyLimit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for AI calls")
		}
	}

	resp, err := http.Post(srv.URL+"/api/v1/diagnose", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("fifth request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fifth status = %d, want 429", resp.StatusCode)
	}
	close(release)
	for i := 0; i < concurrencyLimit; i++ {
		if status := <-results; status != http.StatusOK {
			t.Fatalf("in-flight status = %d, want 200", status)
		}
	}
}
