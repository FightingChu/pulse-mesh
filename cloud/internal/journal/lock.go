package journal

import (
	"fmt"
	"os"
	"path/filepath"
)

type DirectoryLock struct {
	file *os.File
}

func AcquireDirectoryLock(dataDir string) (*DirectoryLock, error) {
	if dataDir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(dataDir, ".pulse-mesh.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open data directory lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("data directory is already locked: %w", err)
	}
	return &DirectoryLock{file: file}, nil
}

func (l *DirectoryLock) Close() error {
	if l == nil {
		return nil
	}
	unlockErr := unlockFile(l.file)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
