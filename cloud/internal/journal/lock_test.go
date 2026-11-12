package journal_test

import (
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
