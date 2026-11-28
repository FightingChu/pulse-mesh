package journal_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pulse-mesh/cloud/internal/journal"
)

func TestRepairRejectsOversizedTailWithoutChangingFile(t *testing.T) {
	const limit = 2 * 1024 * 1024
	for _, complete := range []bool{false, true} {
		for _, prefix := range []string{"", "{\"event_id\":\"saved\"}\n"} {
			t.Run(fmt.Sprintf("complete=%v/prefix=%v", complete, prefix != ""), func(t *testing.T) {
				tail := `{"payload":"` + strings.Repeat("x", limit)
				if complete {
					tail += `"}`
				}
				original := []byte(prefix + tail)
				path := filepath.Join(t.TempDir(), "oversized.jsonl")
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				repaired, repairErr := journal.RepairIncompleteTail(file)
				if repairErr == nil || repaired {
					t.Errorf("oversized tail repair = %v, %v; want rejection without repair", repaired, repairErr)
				}
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(content, original) {
					t.Errorf("oversized tail changed: got %d bytes, want %d", len(content), len(original))
				}
			})
		}
	}
}

func TestRepairPreservesCompleteTailAtValidationLimit(t *testing.T) {
	const limit = 2 * 1024 * 1024
	record := `{"payload":"` + strings.Repeat("x", limit-len(`{"payload":""}`)) + `"}`
	path := filepath.Join(t.TempDir(), "limit.jsonl")
	if err := os.WriteFile(path, []byte(record), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if repaired, err := journal.RepairIncompleteTail(file); !repaired || err != nil {
		t.Fatalf("complete tail at limit: repaired=%v err=%v", repaired, err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != record+"\n" {
		t.Fatal("complete tail at limit was not preserved")
	}
}

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
