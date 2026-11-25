package journal_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pulse-mesh/cloud/internal/journal"
)

func TestRepairPreservesSingleCompleteRecord(t *testing.T) {
	cases := map[string]string{
		"object":          `{"event_id":"first","value":1}`,
		"escaped newline": `{"event_id":"first","message":"line\nnext"}`,
		"whitespace":      " \t{\"event_id\":\"first\"} \t",
		"multiple blocks": `{"payload":"` + strings.Repeat("x", 9000) + `"}`,
	}
	for name, record := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, []byte(record), 0600); err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(path, os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			repaired, err := journal.RepairIncompleteTail(file)
			if err != nil || !repaired {
				t.Fatalf("repair = %v, %v", repaired, err)
			}
			want := record + "\n"
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != want {
				t.Fatalf("complete record was changed: got %d bytes, want %d", len(content), len(want))
			}
			position, err := file.Seek(0, io.SeekCurrent)
			if err != nil || position != int64(len(want)) {
				t.Fatalf("write offset = %d, %v; want %d", position, err, len(want))
			}
			if repaired, err := journal.RepairIncompleteTail(file); repaired || err != nil {
				t.Fatalf("second repair = %v, %v; want unchanged", repaired, err)
			}
			const nextRecord = "{\"event_id\":\"second\"}\n"
			if err := journal.AppendSynced(file, []byte(nextRecord)); err != nil {
				t.Fatal(err)
			}
			content, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != want+nextRecord {
				t.Fatal("append after recovery overwrote the first record")
			}
		})
	}
}
