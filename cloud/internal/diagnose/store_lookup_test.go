package diagnose_test

import (
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
