package alerts_test

import (
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
