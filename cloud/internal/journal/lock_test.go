package journal_test

import (
	"sync"
	"testing"

	"pulse-mesh/cloud/internal/journal"
)

func TestDirectoryLockIsExclusiveAndReleasable(t *testing.T) {
	dir := t.TempDir()
	first, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	if _, err := journal.AcquireDirectoryLock(dir); err == nil {
		t.Fatal("second process lock should fail")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("release first lock: %v", err)
	}
	second, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("release second lock: %v", err)
	}
}

func TestDirectoryLockEmptyPathIsNoop(t *testing.T) {
	lock, err := journal.AcquireDirectoryLock("")
	if err != nil || lock != nil {
		t.Fatalf("lock = %v, err = %v", lock, err)
	}
}

func TestDirectoryLockRejectsSecondHandleForSameDirectory(t *testing.T) {
	dir := t.TempDir()
	first, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := journal.AcquireDirectoryLock(dir); err == nil {
		t.Fatal("second handle must not acquire the lock")
	}
}

func TestDirectoryLockRepeatedCloseDoesNotReleaseNewOwner(t *testing.T) {
	dir := t.TempDir()
	first, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("reacquire after close: %v", err)
	}
	defer second.Close()
	for attempt := 0; attempt < 3; attempt++ {
		if err := first.Close(); err != nil {
			t.Errorf("repeated close %d: %v", attempt, err)
		}
		unexpected, err := journal.AcquireDirectoryLock(dir)
		if err == nil {
			unexpected.Close()
			t.Fatal("closing old lock released the new owner's lock")
		}
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("reacquire after new owner's close: %v", err)
	}
	defer third.Close()
}

func TestDirectoryLockConcurrentCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	lock, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if err := lock.Close(); err != nil {
				t.Errorf("concurrent close: %v", err)
			}
		}()
	}
	close(start)
	workers.Wait()
	next, err := journal.AcquireDirectoryLock(dir)
	if err != nil {
		t.Fatalf("lock not released after concurrent close: %v", err)
	}
	defer next.Close()
}

func TestDirectoryLockEmptyCloseIsNoop(t *testing.T) {
	var absent *journal.DirectoryLock
	if err := absent.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
	var empty journal.DirectoryLock
	for range 2 {
		if err := empty.Close(); err != nil {
			t.Fatalf("zero-value close: %v", err)
		}
	}
}
