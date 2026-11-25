package ingest_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"pulse-mesh/cloud/internal/ingest"
	"pulse-mesh/cloud/internal/journal"
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
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open for simulated torn write: %v", err)
	}
	if _, err := file.WriteString(`{"event_id":"torn`); err != nil {
		t.Fatalf("append torn tail: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close simulated torn write: %v", err)
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

func TestAddBatchDeduplicatesInsideBatchAndAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	store, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	samples := []ingest.Sample{
		{EventID: "batch-1", DeviceID: "motor-01", Metric: "vibration_rms", Value: 1},
		{EventID: "batch-1", DeviceID: "motor-01", Metric: "vibration_rms", Value: 1},
		{EventID: "batch-1", DeviceID: "pump-02", Metric: "vibration_rms", Value: 2},
	}
	added, err := store.AddBatch(samples)
	if err != nil {
		t.Fatalf("add batch: %v", err)
	}
	if len(added) != 3 || !added[0] || added[1] || !added[2] {
		t.Fatalf("added = %v", added)
	}
	if got := store.Count(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	added, err = reopened.AddBatch(samples)
	if err != nil {
		t.Fatalf("retry batch: %v", err)
	}
	for index, wasAdded := range added {
		if wasAdded {
			t.Fatalf("event %d was added again after restart", index)
		}
	}
	if got := reopened.Count(); got != 2 {
		t.Fatalf("reopened count = %d, want 2", got)
	}
}

func TestClosedStoreRejectsWritesAndReadinessButKeepsSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	store, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	event := ingest.Sample{EventID: "close-1", DeviceID: "motor-01", Metric: "vibration", Value: 1}
	if added, err := store.Add(event); err != nil || !added {
		t.Fatalf("add = %v, %v", added, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
	if err := store.Check(); !errors.Is(err, journal.ErrClosed) {
		t.Fatalf("check error = %v, want ErrClosed", err)
	}
	if added, err := store.Add(event); added || !errors.Is(err, journal.ErrClosed) {
		t.Fatalf("write after close = %v, %v", added, err)
	}
	if count := len(store.List("motor-01", 10)); count != 1 {
		t.Fatalf("cached snapshot count = %d, want 1", count)
	}
}

func TestCloseRejectsEveryBatchInBothStorageModes(t *testing.T) {
	for _, mode := range []string{"memory", "journal"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			if mode == "journal" {
				path = filepath.Join(t.TempDir(), "samples.jsonl")
			}
			store, err := ingest.OpenStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			event := ingest.Sample{EventID: "before-close", DeviceID: "motor-01", Metric: "v", Value: 1}
			if added, err := store.Add(event); !added || err != nil {
				t.Fatalf("initial add = %v, %v", added, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("repeated close: %v", err)
			}
			for _, batch := range [][]ingest.Sample{
				nil,
				{},
				{event},
				{{EventID: "after-close", DeviceID: "motor-01", Metric: "v", Value: 2}},
			} {
				if added, err := store.AddBatch(batch); len(added) != 0 || !errors.Is(err, journal.ErrClosed) {
					t.Errorf("batch after close = %v, %v", added, err)
				}
			}
			if err := store.Check(); !errors.Is(err, journal.ErrClosed) {
				t.Errorf("readiness after close: %v", err)
			}
			if store.Count() != 1 || store.List("motor-01", 1)[0].EventID != event.EventID {
				t.Fatal("closed writes changed retained snapshot")
			}
		})
	}
}

func TestConcurrentCloseAndBatchWritesPreserveJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.jsonl")
	store, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	start := make(chan struct{})
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func(sequence int) {
			defer workers.Done()
			<-start
			batch := []ingest.Sample{{EventID: fmt.Sprintf("concurrent-%d", sequence), DeviceID: "motor-01", Metric: "v", Value: float64(sequence)}}
			if _, err := store.AddBatch(batch); err != nil && !errors.Is(err, journal.ErrClosed) {
				t.Errorf("concurrent write: %v", err)
			}
		}(index)
	}
	close(start)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	countAtClose := store.Count()
	workers.Wait()
	if store.Count() != countAtClose {
		t.Fatal("writes changed memory after Close returned")
	}
	reopened, err := ingest.OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Count() != countAtClose {
		t.Fatalf("journal restored %d records, want %d", reopened.Count(), countAtClose)
	}
}
