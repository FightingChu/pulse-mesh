package alerts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/ingest"
)

func TestWindowDedupKeepsIdentifierBoundaries(t *testing.T) {
	for _, mode := range []string{"single", "batch", "restored"} {
		t.Run(mode, func(t *testing.T) {
			first := ingest.Sample{DeviceID: "line|motor", Metric: "vibration", Level: "critical", EventID: "first", Value: 4.8}
			second := ingest.Sample{DeviceID: "line", Metric: "motor|vibration", Level: "critical", EventID: "second", Value: 4.9}
			path := ""
			if mode == "restored" {
				path = filepath.Join(t.TempDir(), "alerts.jsonl")
			}
			engine, err := alerts.OpenStore(path, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			if mode == "batch" {
				created, err := engine.EvaluateBatchWithError([]ingest.Sample{first, second})
				if err != nil || len(created) != 2 {
					t.Fatalf("distinct batch alerts = %+v, %v; want 2", created, err)
				}
			} else {
				if created, err := engine.EvaluateWithError(first); err != nil || created == nil {
					t.Fatalf("first alert = %+v, %v", created, err)
				}
				if mode == "restored" {
					if err := engine.Close(); err != nil {
						t.Fatal(err)
					}
					engine, err = alerts.OpenStore(path, time.Hour)
					if err != nil {
						t.Fatal(err)
					}
					defer engine.Close()
				}
				if created, err := engine.EvaluateWithError(second); err != nil || created == nil {
					t.Fatalf("distinct second alert = %+v, %v", created, err)
				}
			}
			if mode == "restored" {
				if err := engine.Close(); err != nil {
					t.Fatal(err)
				}
				engine, err = alerts.OpenStore(path, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				defer engine.Close()
			}
			for _, retry := range []ingest.Sample{first, second} {
				retry.EventID += "-new-event"
				if created, err := engine.EvaluateWithError(retry); err != nil || created != nil {
					t.Errorf("same window tuple was not suppressed: %+v, %v", created, err)
				}
			}
			if got := len(engine.List("", true)); got != 2 {
				t.Errorf("stored alerts = %d, want 2", got)
			}
		})
	}
}

func TestFaultWindowDedupKeepsIdentifierBoundaries(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "single", true: "batch"}[batch], func(t *testing.T) {
			engine := alerts.New(time.Hour)
			seed := ingest.Sample{DeviceID: "motor", Metric: "east|fault:bearing_wear", Level: "critical", Value: 4.8}
			if created, err := engine.EvaluateWithError(seed); err != nil || created == nil {
				t.Fatalf("seed alert = %+v, %v", created, err)
			}
			if batch {
				created, err := engine.EvaluateFaultEventBatchWithError("motor|east", []alerts.FaultEvent{{EventID: "fault-1", Fault: "bearing_wear"}})
				if err != nil || len(created) != 1 {
					t.Fatalf("distinct fault alert = %+v, %v", created, err)
				}
			} else {
				if created, err := engine.EvaluateFaultEventWithError("motor|east", "fault-1", "bearing_wear"); err != nil || created == nil {
					t.Fatalf("distinct fault alert = %+v, %v", created, err)
				}
			}
			if created, err := engine.EvaluateFaultEventWithError("motor|east", "fault-2", "bearing_wear"); err != nil || created != nil {
				t.Errorf("same fault window tuple was not suppressed: %+v, %v", created, err)
			}
		})
	}
}

func TestLegacyWindowKeyRestoresFromAlertIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.jsonl")
	now := time.Now().UTC()
	record := map[string]any{
		"type": "create", "seen_key": "line|motor|vibration|critical", "seen_at": now,
		"alert": alerts.Alert{ID: "alert-1", DeviceID: "line|motor", Metric: "vibration", Level: "critical", CreatedAt: now},
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := alerts.OpenStore(path, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	same := ingest.Sample{DeviceID: "line|motor", Metric: "vibration", Level: "critical"}
	if created, err := engine.EvaluateWithError(same); err != nil || created != nil {
		t.Fatalf("legacy tuple no longer deduplicates: %+v, %v", created, err)
	}
	distinct := ingest.Sample{DeviceID: "line", Metric: "motor|vibration", Level: "critical"}
	if created, err := engine.EvaluateWithError(distinct); err != nil || created == nil {
		t.Fatalf("legacy key suppressed distinct tuple: %+v, %v", created, err)
	}
}
