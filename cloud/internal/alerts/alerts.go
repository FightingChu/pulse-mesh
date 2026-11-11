package alerts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"pulse-mesh/cloud/internal/ingest"
	"pulse-mesh/cloud/internal/journal"
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
	file    *os.File
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

type journalRecord struct {
	Type    string     `json:"type"`
	Alert   *Alert     `json:"alert,omitempty"`
	ID      string     `json:"id,omitempty"`
	SeenKey string     `json:"seen_key,omitempty"`
	SeenAt  *time.Time `json:"seen_at,omitempty"`
}

func OpenStore(path string, window time.Duration) (*Engine, error) {
	if path == "" {
		return New(window), nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create alert store directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open alert store: %w", err)
	}
	if _, err := journal.RepairIncompleteTail(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("repair alert store: %w", err)
	}
	engine := New(window)
	engine.file = file
	if err := engine.load(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return engine, nil
}

func (e *Engine) load(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek alert store: %w", err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var record journalRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return fmt.Errorf("decode alert store: %w", err)
		}
		switch record.Type {
		case "create":
			if record.Alert == nil {
				return fmt.Errorf("alert create record has no alert")
			}
			e.addMemory(*record.Alert)
			if record.SeenKey != "" && record.SeenAt != nil {
				e.seen[record.SeenKey] = *record.SeenAt
			}
		case "ack":
			e.ackMemory(record.ID, record.AckedAt())
		default:
			return fmt.Errorf("unknown alert journal record %q", record.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read alert store: %w", err)
	}
	_, err := file.Seek(0, io.SeekEnd)
	return err
}

func (r journalRecord) AckedAt() time.Time {
	if r.SeenAt != nil {
		return *r.SeenAt
	}
	return time.Now().UTC()
}

func (e *Engine) addMemory(alert Alert) {
	e.items = append(e.items, alert)
	if number, err := strconv.Atoi(strings.TrimPrefix(alert.ID, "alert-")); err == nil && number > e.seq {
		e.seq = number
	}
	if len(e.items) > e.maxKeep {
		e.items = e.items[len(e.items)-e.maxKeep:]
	}
}

func (e *Engine) appendRecord(record journalRecord) error {
	if e.file == nil {
		return nil
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode alert record: %w", err)
	}
	if err := journal.AppendSynced(e.file, append(encoded, '\n')); err != nil {
		return fmt.Errorf("persist alert record: %w", err)
	}
	return nil
}

func (e *Engine) Evaluate(s ingest.Sample) *Alert {
	alert, _ := e.EvaluateWithError(s)
	return alert
}

func (e *Engine) EvaluateWithError(s ingest.Sample) (*Alert, error) {
	if s.Level != "warning" && s.Level != "critical" {
		return nil, nil
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
	alert, _ := e.EvaluateFaultWithError(deviceID, fault)
	return alert
}

func (e *Engine) EvaluateFaultWithError(deviceID, fault string) (*Alert, error) {
	level := FaultLevel(fault)
	if level == "" {
		return nil, nil
	}
	message := fmt.Sprintf("%s detected on %s", fault, deviceID)
	return e.raise(deviceID, "fault:"+fault, level, 0, message)
}

func (e *Engine) raise(deviceID, metric, level string, value float64, message string) (*Alert, error) {
	key := deviceID + "|" + metric + "|" + level
	now := time.Now().UTC()

	e.mu.Lock()
	defer e.mu.Unlock()

	if last, ok := e.seen[key]; ok && now.Sub(last) < e.window {
		return nil, nil
	}

	alert := Alert{
		ID:        fmt.Sprintf("alert-%d", e.seq+1),
		DeviceID:  deviceID,
		Metric:    metric,
		Level:     level,
		Value:     value,
		Message:   message,
		CreatedAt: now,
	}
	if err := e.appendRecord(journalRecord{Type: "create", Alert: &alert, SeenKey: key, SeenAt: &now}); err != nil {
		return nil, err
	}
	e.seq++
	e.seen[key] = now
	e.addMemory(alert)
	return &alert, nil
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
	acked, _ := e.AckWithError(id)
	return acked
}

func (e *Engine) AckWithError(id string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := range e.items {
		if e.items[i].ID != id {
			continue
		}
		if e.items[i].Acked {
			return true, nil
		}
		now := time.Now().UTC()
		if err := e.appendRecord(journalRecord{Type: "ack", ID: id, SeenAt: &now}); err != nil {
			return false, err
		}
		e.items[i].Acked = true
		e.items[i].AckedAt = &now
		return true, nil
	}
	return false, nil
}

func (e *Engine) ackMemory(id string, at time.Time) {
	for i := range e.items {
		if e.items[i].ID == id {
			e.items[i].Acked = true
			e.items[i].AckedAt = &at
			return
		}
	}
}

func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.file == nil {
		return nil
	}
	return e.file.Close()
}
