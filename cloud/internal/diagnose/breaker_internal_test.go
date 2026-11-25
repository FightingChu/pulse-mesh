package diagnose

import (
	"errors"
	"testing"
	"time"
)

func TestLateSuccessCannotCloseOpenCircuit(t *testing.T) {
	client := NewClientWithBreaker("http://127.0.0.1:1", time.Second, 1, time.Minute)
	lateRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	failedRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	client.finishRequest(failedRequest, false, true)
	client.finishRequest(lateRequest, true, false)
	if _, err := client.beginRequest(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("late success reset circuit: %v", err)
	}
}

func TestLateNeutralRequestCannotReleaseRecoveryProbe(t *testing.T) {
	client := NewClientWithBreaker("http://127.0.0.1:1", time.Second, 1, time.Minute)
	lateRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	failedRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	client.finishRequest(failedRequest, false, true)
	client.mu.Lock()
	client.openUntil = time.Now().Add(-time.Second)
	client.mu.Unlock()
	probe, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	client.finishRequest(lateRequest, false, false)
	if _, err := client.beginRequest(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("late cancellation released active probe: %v", err)
	}
	client.finishRequest(probe, true, false)
	if _, err := client.beginRequest(); err != nil {
		t.Fatalf("successful recovery did not close circuit: %v", err)
	}
}

func TestLateFailureCannotReopenRecoveredCircuit(t *testing.T) {
	client := NewClientWithBreaker("http://127.0.0.1:1", time.Second, 1, time.Minute)
	lateRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	failedRequest, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	client.finishRequest(failedRequest, false, true)
	client.mu.Lock()
	client.openUntil = time.Now().Add(-time.Second)
	client.mu.Unlock()
	probe, err := client.beginRequest()
	if err != nil {
		t.Fatal(err)
	}
	client.finishRequest(probe, true, false)
	client.finishRequest(lateRequest, false, true)
	if _, err := client.beginRequest(); err != nil {
		t.Fatalf("old failure reopened recovered circuit: %v", err)
	}
}
