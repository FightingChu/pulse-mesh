package ingest

import (
	"sync"
	"time"
)

type Sample struct {
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
}

func NewStore() *Store { return &Store{} }

func (s *Store) Add(v Sample) {
	if v.Ts.IsZero() {
		v.Ts = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, v)
	if len(s.items) > 10000 {
		s.items = s.items[len(s.items)-10000:]
	}
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
