package diagnose_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"pulse-mesh/cloud/internal/diagnose"
)

func TestDiagnoseSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spectrum" {
			t.Errorf("path = %q, want /spectrum", r.URL.Path)
		}
		var req diagnose.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.DeviceID != "motor-01" || len(req.Values) != 8 {
			t.Errorf("unexpected request: %+v", req)
		}
		if req.EventID != "boot-1-spectrum-0" {
			t.Errorf("event_id = %q", req.EventID)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"event_id":    req.EventID,
			"device_id":   req.DeviceID,
			"dominant_hz": 25.0,
			"high_band":   0.1,
			"fault":       "imbalance",
		})
	}))
	defer server.Close()

	client := diagnose.NewClient(server.URL)
	result, err := client.Diagnose(context.Background(), diagnose.Request{
		EventID:  "boot-1-spectrum-0",
		DeviceID: "motor-01",
		Values:   []float64{1, 2, 3, 4, 5, 6, 7, 8},
	})
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if result.Fault != "imbalance" || result.DeviceID != "motor-01" || result.EventID != "boot-1-spectrum-0" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DiagnosedAt.IsZero() {
		t.Fatal("diagnosed_at was not stamped")
	}
}

func TestDiagnosePropagatesAIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "values length must be a power of two"})
	}))
	defer server.Close()

	client := diagnose.NewClient(server.URL)
	_, err := client.Diagnose(context.Background(), diagnose.Request{DeviceID: "motor-01", Values: []float64{1, 2, 3}})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "ai service: values length must be a power of two" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreIsBoundedAndNewestFirst(t *testing.T) {
	store := diagnose.NewStore(3)
	for i := 0; i < 5; i++ {
		store.Add(diagnose.Result{DeviceID: "motor-01", Fault: "normal"})
	}
	if got := store.Count(); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if got := len(store.List("motor-01", 10)); got != 3 {
		t.Fatalf("list len = %d, want 3", got)
	}
	if got := len(store.List("pump-02", 10)); got != 0 {
		t.Fatalf("filtered len = %d, want 0", got)
	}
}

func TestStoreDeduplicatesAndGetsEvent(t *testing.T) {
	store := diagnose.NewStore(10)
	result := diagnose.Result{DeviceID: "motor-01", EventID: "boot-1-spectrum-0", Fault: "bearing_wear"}
	if added, err := store.Add(result); !added || err != nil {
		t.Fatal("first result should be stored")
	}
	if added, err := store.Add(result); added || err != nil {
		t.Fatal("retry should be deduplicated")
	}
	got, ok := store.Get("motor-01", "boot-1-spectrum-0")
	if !ok || got.Fault != "bearing_wear" {
		t.Fatalf("get = %+v, %v", got, ok)
	}
	if added, err := store.Add(diagnose.Result{DeviceID: "pump-02", EventID: "boot-1-spectrum-0"}); !added || err != nil {
		t.Fatal("event ID should be scoped per device")
	}
}

func TestOpenStoreRestoresDiagnosisAndEventIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnoses.jsonl")
	first, err := diagnose.OpenStore(path, 10)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	result := diagnose.Result{DeviceID: "motor-01", EventID: "boot-1-spectrum-0", Fault: "bearing_wear"}
	if added, err := first.Add(result); !added || err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := diagnose.OpenStore(path, 10)
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer second.Close()
	if second.Count() != 1 {
		t.Fatalf("restored count = %d, want 1", second.Count())
	}
	if added, err := second.Add(result); added || err != nil {
		t.Fatalf("restored event should deduplicate, added=%v err=%v", added, err)
	}
}
