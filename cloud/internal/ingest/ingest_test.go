package ingest_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"pulse-mesh/cloud/internal/ingest"
)

const firmwareLine = `{"device_id":"motor-01","metric":"vibration_rms","value":4.80,"level":"critical","temp_c":57.0}`

func TestDecodesFirmwareOutput(t *testing.T) {
	var s ingest.Sample
	if err := json.Unmarshal([]byte(firmwareLine), &s); err != nil {
		t.Fatalf("unmarshal firmware line: %v", err)
	}
	if s.DeviceID != "motor-01" || s.Metric != "vibration_rms" {
		t.Fatalf("unexpected identity: %+v", s)
	}
	if s.Value != 4.8 || s.Level != "critical" || s.TempC != 57.0 {
		t.Fatalf("unexpected payload: %+v", s)
	}
}

func TestAddStampsTimestamp(t *testing.T) {
	store := ingest.NewStore()
	if _, err := store.Add(ingest.Sample{DeviceID: "motor-01", Metric: "vibration_rms", Value: 1.2}); err != nil {
		t.Fatal(err)
	}

	got := store.List("motor-01", 10)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Ts.IsZero() {
		t.Fatal("timestamp was not stamped")
	}
}

func TestListFiltersAndOrdersNewestFirst(t *testing.T) {
	store := ingest.NewStore()
	store.Add(ingest.Sample{DeviceID: "motor-01", Metric: "v", Value: 1})
	store.Add(ingest.Sample{DeviceID: "pump-02", Metric: "v", Value: 2})
	store.Add(ingest.Sample{DeviceID: "motor-01", Metric: "v", Value: 3})

	got := store.List("motor-01", 10)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Value != 3 || got[1].Value != 1 {
		t.Fatalf("expected newest first, got %+v", got)
	}

	if all := store.List("", 10); len(all) != 3 {
		t.Fatalf("unfiltered len = %d, want 3", len(all))
	}
}

func TestStoreIsBounded(t *testing.T) {
	store := ingest.NewStore()
	for i := 0; i < 10050; i++ {
		store.Add(ingest.Sample{DeviceID: "motor-01", Metric: "v", Value: float64(i)})
	}
	if got := store.Count(); got != 10000 {
		t.Fatalf("count = %d, want 10000", got)
	}
	got := store.List("motor-01", 1)
	if len(got) != 1 || got[0].Value != 10049 {
		t.Fatalf("expected last value retained, got %+v", got)
	}
}

func TestStoreDeduplicatesEventIDPerDevice(t *testing.T) {
	store := ingest.NewStore()
	first := ingest.Sample{EventID: "boot-1-0", DeviceID: "motor-01", Metric: "vibration_rms", Value: 1}
	if added, err := store.Add(first); !added || err != nil {
		t.Fatal("first event should be stored")
	}
	if added, err := store.Add(first); added || err != nil {
		t.Fatal("retry should be deduplicated")
	}
	secondDevice := first
	secondDevice.DeviceID = "pump-02"
	if added, err := store.Add(secondDevice); !added || err != nil {
		t.Fatal("same event id from another device should be stored")
	}
	if got := store.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
}

func TestOpenStoreRestoresSamplesAndEventIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	first, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	event := ingest.Sample{EventID: "boot-1-0", DeviceID: "motor-01", Metric: "vibration_rms", Value: 1.2}
	if added, err := first.Add(event); !added || err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer second.Close()
	if second.Count() != 1 {
		t.Fatalf("restored count = %d, want 1", second.Count())
	}
	if added, err := second.Add(event); added || err != nil {
		t.Fatalf("restored event should deduplicate, added=%v err=%v", added, err)
	}
}
