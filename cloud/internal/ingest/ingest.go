package ingest

import (
	"sync"
	"time"
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
	mu    sync.Mutex
	items []Sample
	seen  map[string]struct{}
}

func NewStore() *Store { return &Store{seen: make(map[string]struct{})} }

func (s *Store) Add(v Sample) bool {
	if v.Ts.IsZero() {
		v.Ts = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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

func eventKey(deviceID, eventID string) string {
	if eventID == "" {
		return ""
	}
	return deviceID + "\x00" + eventID
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
