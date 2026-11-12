package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
	if len(payload) == 1<<20 {
		return Result{}, true, fmt.Errorf("AI response exceeds 1 MiB limit")
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
	if err := validateResult(req, result); err != nil {
		return Result{}, true, fmt.Errorf("invalid AI response: %w", err)
	}
	result.DiagnosedAt = time.Now().UTC()
	return result, false, nil
}

func validateResult(req Request, result Result) error {
	if result.DeviceID != req.DeviceID {
		return fmt.Errorf("device_id mismatch")
	}
	if result.EventID != req.EventID {
		return fmt.Errorf("event_id mismatch")
	}
	if result.Fault != "normal" && result.Fault != "bearing_wear" && result.Fault != "misalignment" && result.Fault != "imbalance" && result.Fault != "looseness" {
		return fmt.Errorf("unknown fault %q", result.Fault)
	}
	values := []float64{result.DominantHz, result.PeakAmplitude, result.LowBand, result.MidBand, result.HighBand}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("non-finite feature")
		}
	}
	if result.DominantHz < 0 || result.PeakAmplitude < 0 || result.LowBand < 0 || result.MidBand < 0 || result.HighBand < 0 {
		return fmt.Errorf("negative spectrum feature")
	}
	bandTotal := result.LowBand + result.MidBand + result.HighBand
	if bandTotal > 1.05 {
		return fmt.Errorf("band energy ratios sum to %.4f", bandTotal)
	}
	return nil
}
