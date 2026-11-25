package alerts_test

import (
	"path/filepath"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
)

func TestAlertListOwnsAcknowledgementTimestamp(t *testing.T) {
	for _, mode := range []string{"memory", "journal", "restored"} {
		t.Run(mode, func(t *testing.T) {
			path := ""
			if mode != "memory" {
				path = filepath.Join(t.TempDir(), "alerts.jsonl")
			}
			engine, err := alerts.OpenStore(path, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			created, err := engine.EvaluateWithError(sample("critical"))
			if err != nil || created == nil {
				t.Fatalf("create alert = %+v, %v", created, err)
			}
			beforeAck := engine.List("motor-01", false)
			if len(beforeAck) != 1 || beforeAck[0].AckedAt != nil {
				t.Fatalf("unacknowledged snapshot = %+v", beforeAck)
			}
			if acked, err := engine.AckWithError(created.ID); err != nil || !acked {
				t.Fatalf("ack = %v, %v", acked, err)
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
			first := engine.List("motor-01", true)
			second := engine.List("", true)
			if len(first) != 1 || len(second) != 1 || first[0].AckedAt == nil || second[0].AckedAt == nil {
				t.Fatalf("missing acknowledgement timestamp: first=%+v second=%+v", first, second)
			}
			original := *first[0].AckedAt
			*first[0].AckedAt = original.Add(-24 * time.Hour)
			if !second[0].AckedAt.Equal(original) {
				t.Error("mutating one snapshot changed another snapshot")
			}
			*second[0].AckedAt = original.Add(24 * time.Hour)
			current := engine.List("motor-01", true)
			if len(current) != 1 || current[0].AckedAt == nil || !current[0].AckedAt.Equal(original) {
				t.Errorf("mutating snapshots changed the stored acknowledgement: %+v", current)
			}
			if beforeAck[0].Acked || beforeAck[0].AckedAt != nil {
				t.Error("acknowledgement changed an earlier snapshot")
			}
			if len(engine.List("motor-01", false)) != 0 {
				t.Error("acknowledged alert leaked into the default list")
			}
		})
	}
}
