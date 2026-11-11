package journal_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"pulse-mesh/cloud/internal/journal"
)

func TestRepairIncompleteTailKeepsCompleteRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"ok\":1}\n{\"partial\":"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	repaired, err := journal.RepairIncompleteTail(file)
	if err != nil || !repaired {
		t.Fatalf("repair = %v, %v", repaired, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"ok\":1}\n" {
		t.Fatalf("file after repair = %q", got)
	}
}

type failingSyncFile struct {
	*os.File
}

func (f failingSyncFile) Sync() error {
	return errors.New("simulated sync failure")
}

func TestAppendSyncedRollsBackWhenSyncFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"ok\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = journal.AppendSynced(failingSyncFile{file}, []byte("{\"new\":2}\n"))
	if err == nil {
		t.Fatal("expected sync failure")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\"ok\":1}\n" {
		t.Fatalf("journal after rollback = %q", got)
	}
}

func TestConcurrentAppendsRemainWholeJSONLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	const writers = 32
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			line := []byte(fmt.Sprintf("{\"writer\":%d,\"payload\":\"%s\"}\n", index, strings.Repeat("x", 4096)))
			if err := journal.AppendSynced(file, line); err != nil {
				t.Errorf("append writer %d: %v", index, err)
			}
		}(i)
	}
	wait.Wait()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != writers {
		t.Fatalf("line count = %d, want %d", len(lines), writers)
	}
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("corrupt JSONL record: %v", err)
		}
	}
}

func TestRepairIncompleteOnlyRecordTruncatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"partial\":"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	repaired, err := journal.RepairIncompleteTail(file)
	if err != nil || !repaired {
		t.Fatalf("repair = %v, %v", repaired, err)
	}
	info, err := file.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatalf("size = %v, err=%v", info.Size(), err)
	}
}

func TestRepairCompleteFileDoesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	repaired, err := journal.RepairIncompleteTail(file)
	if err != nil || repaired {
		t.Fatalf("repair = %v, %v", repaired, err)
	}
}
