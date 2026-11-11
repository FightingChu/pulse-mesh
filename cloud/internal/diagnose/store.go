package diagnose

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"pulse-mesh/cloud/internal/journal"
)

type Store struct {
	mu           sync.Mutex
	items        []Result
	limit        int
	seen         map[string]struct{}
	file         *os.File
	writesFailed uint64
}

func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 1000
	}
	return &Store{limit: limit, seen: make(map[string]struct{})}
}

func OpenStore(path string, limit int) (*Store, error) {
	if path == "" {
		return NewStore(limit), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create diagnosis store directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open diagnosis store: %w", err)
	}
	if _, err := journal.RepairIncompleteTail(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("repair diagnosis store: %w", err)
	}
	store := NewStore(limit)
	store.file = file
	if err := store.load(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) load(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek diagnosis store: %w", err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var result Result
		if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
			return fmt.Errorf("decode diagnosis store: %w", err)
		}
		s.addMemory(result)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read diagnosis store: %w", err)
	}
	_, err := file.Seek(0, io.SeekEnd)
	return err
}

func (s *Store) addMemory(r Result) bool {
	key := resultKey(r.DeviceID, r.EventID)
	if key != "" {
		if _, exists := s.seen[key]; exists {
			return false
		}
		s.seen[key] = struct{}{}
	}
	s.items = append(s.items, r)
	if len(s.items) > s.limit {
		removed := s.items[0]
		if removedKey := resultKey(removed.DeviceID, removed.EventID); removedKey != "" {
			delete(s.seen, removedKey)
		}
		s.items = s.items[len(s.items)-s.limit:]
	}
	return true
}

func (s *Store) Add(r Result) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := resultKey(r.DeviceID, r.EventID)
	if key != "" {
		if _, exists := s.seen[key]; exists {
			return false, nil
		}
	}
	if s.file != nil {
		encoded, err := json.Marshal(r)
		if err != nil {
			return false, fmt.Errorf("encode diagnosis: %w", err)
		}
		if err := journal.AppendSynced(s.file, append(encoded, '\n')); err != nil {
			s.writesFailed++
			return false, fmt.Errorf("persist diagnosis: %w", err)
		}
	}
	return s.addMemory(r), nil
}

func (s *Store) Get(device, eventID string) (Result, bool) {
	key := resultKey(device, eventID)
	if key == "" {
		return Result{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.items) - 1; i >= 0; i-- {
		if resultKey(s.items[i].DeviceID, s.items[i].EventID) == key {
			return s.items[i], true
		}
	}
	return Result{}, false
}

func resultKey(deviceID, eventID string) string {
	if eventID == "" {
		return ""
	}
	return deviceID + "\x00" + eventID
}

func (s *Store) List(device string, limit int) []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	out := make([]Result, 0, limit)
	for i := len(s.items) - 1; i >= 0 && len(out) < limit; i-- {
		if device == "" || s.items[i].DeviceID == device {
			out = append(out, s.items[i])
		}
	}
	return out
}

func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

func (s *Store) Stats() (int, int64, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var bytes int64
	if s.file != nil {
		if info, err := s.file.Stat(); err == nil {
			bytes = info.Size()
		}
	}
	return len(s.items), bytes, s.writesFailed
}

func (s *Store) Check() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	if _, err := s.file.Stat(); err != nil {
		return err
	}
	return s.file.Sync()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	return s.file.Close()
}
