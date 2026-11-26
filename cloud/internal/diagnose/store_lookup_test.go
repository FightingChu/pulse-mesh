package diagnose_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"pulse-mesh/cloud/internal/diagnose"
)

func TestGetAfterRetentionEviction(t *testing.T) {
	store := diagnose.NewStore(2)
	for _, device := range []string{"motor-01", "motor-02", "motor-03"} {
		if added, err := store.Add(diagnose.Result{DeviceID: device, EventID: "same-event"}); err != nil || !added {
			t.Fatalf("add %s = %v, %v", device, added, err)
		}
	}
	if _, ok := store.Get("motor-01", "same-event"); ok {
		t.Fatal("evicted event still found")
	}
	for _, device := range []string{"motor-02", "motor-03"} {
		if got, ok := store.Get(device, "same-event"); !ok || got.DeviceID != device || got.EventID != "same-event" {
			t.Fatalf("get %s = %+v, %v", device, got, ok)
		}
	}
}

func BenchmarkStoreGetMissing(b *testing.B) {
	for _, size := range []int{1, 1000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			store := diagnose.NewStore(size)
			for index := range size {
				if _, err := store.Add(diagnose.Result{DeviceID: "motor-01", EventID: strconv.Itoa(index)}); err != nil {
					b.Fatal(err)
				}
			}
			for b.Loop() {
				if _, found := store.Get("motor-01", "new-event"); found {
					b.Fatal("unexpected lookup hit")
				}
			}
		})
	}
}

func TestGetRetainedResultsAfterReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnoses.jsonl")
	const limit = 3
	store, err := diagnose.OpenStore(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := range 12 {
		event := strconv.Itoa(index % 4)
		if index%3 == 0 {
			event = ""
		}
		result := diagnose.Result{DeviceID: "motor", EventID: event, PeakAmplitude: float64(index)}
		if added, err := store.Add(result); !added || err != nil {
			t.Fatalf("add at %d = %t, %v", index, added, err)
		}
		if event != "" {
			result.PeakAmplitude = -1
			if added, err := store.Add(result); added || err != nil {
				t.Fatalf("duplicate at %d = %t, %v", index, added, err)
			}
		}
	}
	check := func(store *diagnose.Store) {
		t.Helper()
		for _, event := range []string{"", "0", "1", "2", "3"} {
			got, found := store.Get("motor", event)
			switch event {
			case "2", "3":
				want := 10.0
				if event == "3" {
					want = 11.0
				}
				if !found || got.DeviceID != "motor" || got.EventID != event || got.PeakAmplitude != want {
					t.Errorf("lookup %q = %+v, %t; want original amplitude %g", event, got, found, want)
				}
			default:
				if found {
					t.Errorf("empty or evicted event %q was returned", event)
				}
			}
		}
	}
	check(store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := diagnose.OpenStore(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	check(restored)
}

func BenchmarkStoreGetRetained(b *testing.B) {
	const size = 1000
	store := diagnose.NewStore(size)
	for index := range size {
		if _, err := store.Add(diagnose.Result{DeviceID: "motor", EventID: strconv.Itoa(index)}); err != nil {
			b.Fatal(err)
		}
	}
	for _, index := range []int{0, size / 2, size - 1} {
		event := strconv.Itoa(index)
		b.Run(event, func(b *testing.B) {
			for b.Loop() {
				if got, ok := store.Get("motor", event); !ok || got.EventID != event {
					b.Fatal("unexpected lookup result")
				}
			}
		})
	}
}
