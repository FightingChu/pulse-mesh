package journal_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/ingest"
)

func TestStoresRejectOversizedTailWithoutLosingData(t *testing.T) {
	for _, scenario := range []struct {
		name string
		open func(string) error
	}{
		{"samples", func(path string) error {
			store, err := ingest.OpenStore(path)
			if err == nil {
				defer store.Close()
			}
			return err
		}},
		{"diagnoses", func(path string) error {
			store, err := diagnose.OpenStore(path, 10)
			if err == nil {
				defer store.Close()
			}
			return err
		}},
		{"alerts", func(path string) error {
			store, err := alerts.OpenStore(path, time.Minute)
			if err == nil {
				defer store.Close()
			}
			return err
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), scenario.name+".jsonl")
			original := []byte(`{"padding":"` + strings.Repeat("x", 2*1024*1024) + `"}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := scenario.open(path); err == nil {
				t.Error("store opened successfully after an unvalidated oversized tail")
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(content, original) {
				t.Errorf("store initialization changed journal: got %d bytes, want %d", len(content), len(original))
			}
		})
	}
}

func omitFinalDelimiter(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(content, []byte{'\n'}) {
		t.Fatal("fixture does not end with a delimiter")
	}
	if err := os.WriteFile(path, content[:len(content)-1], 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStoresRestoreSingleRecordWithoutNewline(t *testing.T) {
	t.Run("samples", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "samples.jsonl")
		store, err := ingest.OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		event := ingest.Sample{EventID: "first", DeviceID: "motor-01", Metric: "vibration_rms", Value: 1.2}
		if added, err := store.Add(event); !added || err != nil {
			t.Fatalf("initial write = %v, %v", added, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		omitFinalDelimiter(t, path)
		restored, err := ingest.OpenStore(path)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		items := restored.List("motor-01", 1)
		if len(items) != 1 || items[0].EventID != event.EventID || items[0].Value != event.Value {
			t.Fatalf("restored samples = %+v", items)
		}
		if added, err := restored.Add(event); added || err != nil {
			t.Fatalf("duplicate after recovery = %v, %v", added, err)
		}
	})
	t.Run("diagnoses", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "diagnoses.jsonl")
		store, err := diagnose.OpenStore(path, 10)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		event := diagnose.Result{DeviceID: "motor-01", EventID: "first", Fault: "bearing_wear"}
		if added, err := store.Add(event); !added || err != nil {
			t.Fatalf("initial write = %v, %v", added, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		omitFinalDelimiter(t, path)
		restored, err := diagnose.OpenStore(path, 10)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		result, found := restored.Get(event.DeviceID, event.EventID)
		if !found || result.Fault != event.Fault {
			t.Fatalf("restored diagnosis = %+v, found=%v", result, found)
		}
		if added, err := restored.Add(event); added || err != nil {
			t.Fatalf("duplicate after recovery = %v, %v", added, err)
		}
	})
	t.Run("alerts", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "alerts.jsonl")
		engine, err := alerts.OpenStore(path, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = engine.Close() })
		event := ingest.Sample{EventID: "first", DeviceID: "motor-01", Metric: "vibration_rms", Level: "critical", Value: 4.8}
		created, err := engine.EvaluateWithError(event)
		if created == nil || err != nil {
			t.Fatalf("initial alert = %+v, %v", created, err)
		}
		if err := engine.Close(); err != nil {
			t.Fatal(err)
		}
		omitFinalDelimiter(t, path)
		restored, err := alerts.OpenStore(path, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		defer restored.Close()
		items := restored.List(event.DeviceID, true)
		if len(items) != 1 || items[0].ID != created.ID || items[0].EventID != event.EventID {
			t.Fatalf("restored alerts = %+v", items)
		}
		if duplicate, err := restored.EvaluateWithError(event); duplicate != nil || err != nil {
			t.Fatalf("duplicate after recovery = %+v, %v", duplicate, err)
		}
	})
}
