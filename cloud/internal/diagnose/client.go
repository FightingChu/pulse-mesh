package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("AI diagnosis circuit is open")

type Request struct {
	EventID      string    `json:"event_id,omitempty"`
	DeviceID     string    `json:"device_id"`
	Values       []float64 `json:"values"`
	SampleRateHz float64   `json:"sample_rate_hz"`
	RotationHz   float64   `json:"rotation_hz"`
}

type Result struct {
	EventID       string    `json:"event_id,omitempty"`
	DeviceID      string    `json:"device_id"`
	DominantHz    float64   `json:"dominant_hz"`
	PeakAmplitude float64   `json:"peak_amplitude"`
	LowBand       float64   `json:"low_band"`
	MidBand       float64   `json:"mid_band"`
	HighBand      float64   `json:"high_band"`
	Fault         string    `json:"fault"`
	DiagnosedAt   time.Time `json:"diagnosed_at"`
}

type Client struct {
	base             string
	hc               *http.Client
	mu               sync.Mutex
	failureThreshold int
	failureCount     int
	cooldown         time.Duration
	openUntil        time.Time
	probeInFlight    bool
}

func NewClient(base string) *Client {
	return NewClientWithBreaker(base, 10*time.Second, 3, 15*time.Second)
}

func NewClientWithBreaker(base string, timeout time.Duration, failureThreshold int, cooldown time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 15 * time.Second
	}
	return &Client{
		base:             strings.TrimRight(base, "/"),
		hc:               &http.Client{Timeout: timeout},
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
	}
}

func (c *Client) Diagnose(ctx context.Context, req Request) (Result, error) {
	probe, err := c.beginRequest()
	if err != nil {
		return Result{}, err
	}
	result, countFailure, err := c.doDiagnose(ctx, req)
	c.finishRequest(probe, countFailure && err != nil)
	return result, err
}

func (c *Client) beginRequest() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.openUntil.IsZero() {
		return false, nil
	}
	if time.Now().Before(c.openUntil) {
		return false, ErrCircuitOpen
	}
	if c.probeInFlight {
		return false, ErrCircuitOpen
	}
	c.probeInFlight = true
	return true, nil
}

func (c *Client) finishRequest(probe, failed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !failed {
		c.failureCount = 0
		c.openUntil = time.Time{}
		c.probeInFlight = false
		return
	}
	c.failureCount++
	c.probeInFlight = false
	if probe || c.failureCount >= c.failureThreshold {
		c.openUntil = time.Now().Add(c.cooldown)
	}
}

func (c *Client) doDiagnose(ctx context.Context, req Request) (Result, bool, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, false, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/spectrum", bytes.NewReader(body))
	if err != nil {
		return Result{}, false, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return Result{}, true, fmt.Errorf("call ai service: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, true, fmt.Errorf("read ai response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &apiErr) == nil && apiErr.Error != "" {
			return Result{}, resp.StatusCode >= http.StatusInternalServerError, fmt.Errorf("ai service: %s", apiErr.Error)
		}
		return Result{}, resp.StatusCode >= http.StatusInternalServerError, fmt.Errorf("ai service returned %d", resp.StatusCode)
	}

	var result Result
	if err := json.Unmarshal(payload, &result); err != nil {
		return Result{}, true, fmt.Errorf("decode ai response: %w", err)
	}
	if result.DeviceID == "" {
		result.DeviceID = req.DeviceID
	}
	if result.EventID == "" {
		result.EventID = req.EventID
	}
	result.DiagnosedAt = time.Now().UTC()
	return result, false, nil
}
