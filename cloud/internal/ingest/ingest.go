package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"pulse-mesh/cloud/internal/journal"
)

type Sample struct {
	EventID  string    `json:"event_id,omitempty"`
	DeviceID string    `json:"device_id"`
	Metric   string    `json:"metric"`
	Value    float64   `json:"value"`
	Level    string    `json:"level,omitempty"`
	TempC    float64   `json:"temp_c,omitempty"`
	Ts       time.Time `json:"ts"`
}

type Store struct {
	mu           sync.Mutex
	items        []Sample
	seen         map[string]struct{}
	file         *os.File
	writesFailed uint64
}

func NewStore() *Store { return &Store{seen: make(map[string]struct{})} }

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return NewStore(), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create sample store directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open sample store: %w", err)
	}
	if _, err := journal.RepairIncompleteTail(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("repair sample store: %w", err)
	}
	store := &Store{seen: make(map[string]struct{}), file: file}
	if err := store.load(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) load(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek sample store: %w", err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var sample Sample
		if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
			return fmt.Errorf("decode sample store: %w", err)
		}
		s.addMemory(sample)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read sample store: %w", err)
	}
	_, err := file.Seek(0, io.SeekEnd)
	return err
}

func (s *Store) addMemory(v Sample) bool {
	key := eventKey(v.DeviceID, v.EventID)
	if key != "" {
		if _, exists := s.seen[key]; exists {
			return false
		}
		s.seen[key] = struct{}{}
	}
	s.items = append(s.items, v)
	if len(s.items) > 10000 {
		removed := s.items[0]
		if removedKey := eventKey(removed.DeviceID, removed.EventID); removedKey != "" {
			delete(s.seen, removedKey)
		}
		s.items = s.items[len(s.items)-10000:]
	}
	return true
}

func (s *Store) Add(v Sample) (bool, error) {
	added, err := s.AddBatch([]Sample{v})
	if err != nil {
		return false, err
	}
	return added[0], nil
}

func (s *Store) AddBatch(samples []Sample) ([]bool, error) {
	if len(samples) == 0 {
		return []bool{}, nil
	}
	prepared := append([]Sample(nil), samples...)
	for i := range prepared {
		if prepared[i].Ts.IsZero() {
			prepared[i].Ts = time.Now().UTC()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	added := make([]bool, len(prepared))
	batchSeen := make(map[string]struct{}, len(prepared))
	encoded := make([][]byte, 0, len(prepared))
	for i, sample := range prepared {
		key := eventKey(sample.DeviceID, sample.EventID)
		if key != "" {
			if _, exists := s.seen[key]; exists {
				continue
			}
			if _, exists := batchSeen[key]; exists {
				continue
			}
			batchSeen[key] = struct{}{}
		}
		added[i] = true
		if s.file != nil {
			line, err := json.Marshal(sample)
			if err != nil {
				return nil, fmt.Errorf("encode sample %d: %w", i, err)
			}
			encoded = append(encoded, append(line, '\n'))
		}
	}

	if s.file != nil && len(encoded) > 0 {
		var record []byte
		for _, line := range encoded {
			record = append(record, line...)
		}
		if err := journal.AppendSynced(s.file, record); err != nil {
			s.writesFailed++
			return nil, fmt.Errorf("persist sample batch: %w", err)
		}
	}
	for i, sample := range prepared {
		if added[i] {
			s.addMemory(sample)
		}
	}
	return added, nil
}

func eventKey(deviceID, eventID string) string {
	if eventID == "" {
		return ""
	}
	return deviceID + "\x00" + eventID
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

func (s *Store) List(device string, n int) []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Sample
	for i := len(s.items) - 1; i >= 0 && len(out) < n; i-- {
		if device == "" || s.items[i].DeviceID == device {
			out = append(out, s.items[i])
		}
	}
	return out
}

func (s *Store) Count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.items) }

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
