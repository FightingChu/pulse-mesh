package alerts_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/ingest"
)

func sample(level string) ingest.Sample {
	return ingest.Sample{
		DeviceID: "motor-01",
		Metric:   "vibration_rms",
		Value:    4.8,
		Level:    level,
	}
}

func TestNormalSampleCreatesNoAlert(t *testing.T) {
	engine := alerts.New(time.Minute)
	if got := engine.Evaluate(sample("normal")); got != nil {
		t.Fatalf("expected nil alert, got %+v", got)
	}
	if got := engine.Evaluate(sample("")); got != nil {
		t.Fatalf("expected nil alert for empty level, got %+v", got)
	}
}

func TestWarningCreatesAlert(t *testing.T) {
	engine := alerts.New(time.Minute)
	alert := engine.Evaluate(sample("warning"))
	if alert == nil {
		t.Fatal("expected an alert")
	}
	if alert.Level != "warning" || alert.DeviceID != "motor-01" || alert.Acked {
		t.Fatalf("unexpected alert: %+v", alert)
	}
	if alert.Message == "" || alert.ID == "" {
		t.Fatalf("alert missing fields: %+v", alert)
	}
}

func TestDedupWithinWindow(t *testing.T) {
	engine := alerts.New(time.Minute)
	if engine.Evaluate(sample("critical")) == nil {
		t.Fatal("first alert should be created")
	}
	if got := engine.Evaluate(sample("critical")); got != nil {
		t.Fatalf("duplicate should be suppressed, got %+v", got)
	}
	if got := len(engine.List("", false)); got != 1 {
		t.Fatalf("len = %d, want 1", got)
	}
}

func TestDifferentLevelIsNotDeduped(t *testing.T) {
	engine := alerts.New(time.Minute)
	engine.Evaluate(sample("warning"))
	if engine.Evaluate(sample("critical")) == nil {
		t.Fatal("critical should not be deduped against warning")
	}
	if got := len(engine.List("", false)); got != 2 {
		t.Fatalf("len = %d, want 2", got)
	}
}

func TestAckHidesFromDefaultList(t *testing.T) {
	engine := alerts.New(time.Minute)
	alert := engine.Evaluate(sample("critical"))
	if !engine.Ack(alert.ID) {
		t.Fatal("ack should succeed")
	}
	if !engine.Ack(alert.ID) {
		t.Fatal("repeated ack should be idempotent")
	}
	if got := len(engine.List("", false)); got != 0 {
		t.Fatalf("default list len = %d, want 0", got)
	}
	if got := len(engine.List("", true)); got != 1 {
		t.Fatalf("include_acked len = %d, want 1", got)
	}
	if engine.Ack("missing") {
		t.Fatal("ack of unknown id should fail")
	}
}

func TestListFiltersByDevice(t *testing.T) {
	engine := alerts.New(time.Minute)
	engine.Evaluate(sample("critical"))
	other := sample("critical")
	other.DeviceID = "pump-02"
	engine.Evaluate(other)

	if got := len(engine.List("motor-01", false)); got != 1 {
		t.Fatalf("motor len = %d, want 1", got)
	}
	if got := len(engine.List("", false)); got != 2 {
		t.Fatalf("all len = %d, want 2", got)
	}
}

func TestOpenStoreRestoresAckAndDedupState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.jsonl")
	first, err := alerts.OpenStore(path, time.Hour)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	created, err := first.EvaluateWithError(sample("critical"))
	if err != nil || created == nil {
		t.Fatalf("create alert = %+v, %v", created, err)
	}
	if acked, err := first.AckWithError(created.ID); err != nil || !acked {
		t.Fatalf("ack = %v, %v", acked, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := alerts.OpenStore(path, time.Hour)
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	defer second.Close()
	if got := len(second.List("", false)); got != 0 {
		t.Fatalf("open alert count = %d, want 0", got)
	}
	all := second.List("", true)
	if len(all) != 1 || !all[0].Acked || all[0].ID != created.ID {
		t.Fatalf("restored alert = %+v", all)
	}
	if duplicate, err := second.EvaluateWithError(sample("critical")); err != nil || duplicate != nil {
		t.Fatalf("dedup after restart = %+v, %v", duplicate, err)
	}
	next := sample("warning")
	createdNext, err := second.EvaluateWithError(next)
	if err != nil || createdNext == nil || createdNext.ID != "alert-2" {
		t.Fatalf("next alert = %+v, %v", createdNext, err)
	}
}

func TestFailedJournalWriteDoesNotConsumeAlert(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.jsonl")
	engine, err := alerts.OpenStore(path, time.Minute)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if alert, err := engine.EvaluateWithError(sample("critical")); err == nil || alert != nil {
		t.Fatalf("closed journal write = %+v, %v; want error", alert, err)
	}

	recovered, err := alerts.OpenStore(path, time.Minute)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer recovered.Close()
	alert, err := recovered.EvaluateWithError(sample("critical"))
	if err != nil || alert == nil || alert.ID != "alert-1" {
		t.Fatalf("retry after restart = %+v, %v", alert, err)
	}
}

func TestSeenIndexIsBoundedByAlertRetention(t *testing.T) {
	engine := alerts.New(time.Hour)
	for index := 0; index < 1100; index++ {
		device := fmt.Sprintf("motor-%04d", index)
		if alert := engine.Evaluate(sampleForDevice(device, "critical")); alert == nil {
			t.Fatalf("alert %d was unexpectedly deduplicated", index)
		}
	}
	if got := len(engine.List("", true)); got != 1000 {
		t.Fatalf("retained alerts = %d, want 1000", got)
	}
	if alert := engine.Evaluate(sampleForDevice("motor-0000", "critical")); alert == nil {
		t.Fatal("event older than the bounded retention window should no longer be deduplicated")
	}
}

func sampleForDevice(deviceID, level string) ingest.Sample {
	s := sample(level)
	s.DeviceID = deviceID
	return s
}
