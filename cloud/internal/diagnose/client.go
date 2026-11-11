package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

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
	base string
	hc   *http.Client
}

func NewClient(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Diagnose(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/spectrum", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("call ai service: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("read ai response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &apiErr) == nil && apiErr.Error != "" {
			return Result{}, fmt.Errorf("ai service: %s", apiErr.Error)
		}
		return Result{}, fmt.Errorf("ai service returned %d", resp.StatusCode)
	}

	var result Result
	if err := json.Unmarshal(payload, &result); err != nil {
		return Result{}, fmt.Errorf("decode ai response: %w", err)
	}
	if result.DeviceID == "" {
		result.DeviceID = req.DeviceID
	}
	if result.EventID == "" {
		result.EventID = req.EventID
	}
	result.DiagnosedAt = time.Now().UTC()
	return result, nil
}
