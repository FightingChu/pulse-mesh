package diagnose

import "sync"

type Store struct {
	mu    sync.Mutex
	items []Result
	limit int
}

func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 1000
	}
	return &Store{limit: limit}
}

func (s *Store) Add(r Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, r)
	if len(s.items) > s.limit {
		s.items = s.items[len(s.items)-s.limit:]
	}
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
