package alerts

import (
	"fmt"
	"sync"
	"time"

	"pulse-mesh/cloud/internal/ingest"
)

type Alert struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	Metric    string     `json:"metric"`
	Level     string     `json:"level"`
	Value     float64    `json:"value"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
	Acked     bool       `json:"acked"`
	AckedAt   *time.Time `json:"acked_at,omitempty"`
}

type Engine struct {
	mu      sync.Mutex
	items   []Alert
	seen    map[string]time.Time
	window  time.Duration
	seq     int
	maxKeep int
}

func New(window time.Duration) *Engine {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &Engine{
		seen:    make(map[string]time.Time),
		window:  window,
		maxKeep: 1000,
	}
}

func (e *Engine) Evaluate(s ingest.Sample) *Alert {
	if s.Level != "warning" && s.Level != "critical" {
		return nil
	}

	key := s.DeviceID + "|" + s.Metric + "|" + s.Level
	now := time.Now().UTC()

	e.mu.Lock()
	defer e.mu.Unlock()

	if last, ok := e.seen[key]; ok && now.Sub(last) < e.window {
		return nil
	}
	e.seen[key] = now

	e.seq++
	alert := Alert{
		ID:        fmt.Sprintf("alert-%d", e.seq),
		DeviceID:  s.DeviceID,
		Metric:    s.Metric,
		Level:     s.Level,
		Value:     s.Value,
		Message:   fmt.Sprintf("%s on %s is %s (%.2f)", s.Metric, s.DeviceID, s.Level, s.Value),
		CreatedAt: now,
	}
	e.items = append(e.items, alert)
	if len(e.items) > e.maxKeep {
		e.items = e.items[len(e.items)-e.maxKeep:]
	}
	return &alert
}

func (e *Engine) List(device string, includeAcked bool) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]Alert, 0, len(e.items))
	for _, a := range e.items {
		if device != "" && a.DeviceID != device {
			continue
		}
		if !includeAcked && a.Acked {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (e *Engine) Ack(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.items {
		if e.items[i].ID != id {
			continue
		}
		if e.items[i].Acked {
			return true
		}
		now := time.Now().UTC()
		e.items[i].Acked = true
		e.items[i].AckedAt = &now
		return true
	}
	return false
}
