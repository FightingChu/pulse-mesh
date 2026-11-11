package diagnose

import "sync"

type Store struct {
	mu    sync.Mutex
	items []Result
	limit int
	seen  map[string]struct{}
}

func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 1000
	}
	return &Store{limit: limit, seen: make(map[string]struct{})}
}

func (s *Store) Add(r Result) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
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
