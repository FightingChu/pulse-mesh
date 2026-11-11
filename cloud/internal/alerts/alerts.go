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
	message := fmt.Sprintf("%s on %s is %s (%.2f)", s.Metric, s.DeviceID, s.Level, s.Value)
	return e.raise(s.DeviceID, s.Metric, s.Level, s.Value, message)
}

func FaultLevel(fault string) string {
	switch fault {
	case "bearing_wear", "misalignment":
		return "critical"
	case "imbalance", "looseness":
		return "warning"
	default:
		return ""
	}
}

func (e *Engine) EvaluateFault(deviceID, fault string) *Alert {
	level := FaultLevel(fault)
	if level == "" {
		return nil
	}
	message := fmt.Sprintf("%s detected on %s", fault, deviceID)
	return e.raise(deviceID, "fault:"+fault, level, 0, message)
}

func (e *Engine) raise(deviceID, metric, level string, value float64, message string) *Alert {
	key := deviceID + "|" + metric + "|" + level
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
		DeviceID:  deviceID,
		Metric:    metric,
		Level:     level,
		Value:     value,
		Message:   message,
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
