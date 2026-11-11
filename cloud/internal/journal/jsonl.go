package journal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const scanChunk = 4096

var appendLocks sync.Map

type SyncFile interface {
	Stat() (os.FileInfo, error)
	Seek(int64, int) (int64, error)
	Write([]byte) (int, error)
	Sync() error
	Truncate(int64) error
}

func RepairIncompleteTail(file *os.File) (bool, error) {
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	size := info.Size()
	if size == 0 {
		return false, nil
	}
	var last [1]byte
	if _, err := file.ReadAt(last[:], size-1); err != nil {
		return false, err
	}
	if last[0] == '\n' {
		_, err := file.Seek(0, io.SeekEnd)
		return false, err
	}

	buffer := make([]byte, scanChunk)
	for end := size; end > 0; {
		start := end - scanChunk
		if start < 0 {
			start = 0
		}
		length := int(end - start)
		if _, err := file.ReadAt(buffer[:length], start); err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		for i := length - 1; i >= 0; i-- {
			if buffer[i] == '\n' {
				truncateAt := start + int64(i) + 1
				if err := file.Truncate(truncateAt); err != nil {
					return false, err
				}
				_, err := file.Seek(0, io.SeekEnd)
				return true, err
			}
		}
		end = start
	}
	if err := file.Truncate(0); err != nil {
		return false, err
	}
	_, err = file.Seek(0, io.SeekEnd)
	return true, err
}

func AppendSynced(file SyncFile, record []byte) error {
	if regularFile, ok := file.(*os.File); ok {
		lockValue, _ := appendLocks.LoadOrStore(regularFile.Name(), &sync.Mutex{})
		lock := lockValue.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	start := info.Size()
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	n, writeErr := file.Write(record)
	if writeErr == nil && n != len(record) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	if writeErr == nil {
		return nil
	}
	truncateErr := file.Truncate(start)
	_, seekErr := file.Seek(0, io.SeekEnd)
	if truncateErr != nil {
		return errors.Join(writeErr, fmt.Errorf("rollback incomplete journal record: %w", truncateErr))
	}
	if seekErr != nil {
		return errors.Join(writeErr, fmt.Errorf("seek journal after rollback: %w", seekErr))
	}
	if syncErr := file.Sync(); syncErr != nil {
		return errors.Join(writeErr, fmt.Errorf("sync journal rollback: %w", syncErr))
	}
	return writeErr
}
