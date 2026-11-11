package main

import "testing"

func TestSpikeTickIsCritical(t *testing.T) {
	s := reading(7)
	if s.Level != "critical" {
		t.Fatalf("level = %q, want critical", s.Level)
	}
	if s.Value < 4 {
		t.Fatalf("value = %.2f, want >= 4", s.Value)
	}
}

func TestNormalTick(t *testing.T) {
	s := reading(1)
	if s.Level != "normal" {
		t.Fatalf("level = %q, want normal", s.Level)
	}
}

func TestLevelNameThresholds(t *testing.T) {
	cases := map[float64]string{1.0: "normal", 3.0: "warning", 4.5: "critical"}
	for rms, want := range cases {
		if got := levelName(rms); got != want {
			t.Fatalf("levelName(%.1f) = %q, want %q", rms, got, want)
		}
	}
}
