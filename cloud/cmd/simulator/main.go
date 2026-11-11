package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"
)

type sample struct {
	DeviceID string  `json:"device_id"`
	Metric   string  `json:"metric"`
	Value    float64 `json:"value"`
	Level    string  `json:"level"`
	TempC    float64 `json:"temp_c"`
}

func levelName(rms float64) string {
	switch {
	case rms > 4.0:
		return "critical"
	case rms > 2.5:
		return "warning"
	default:
		return "normal"
	}
}

func rms(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(values)))
}

func reading(tick int) sample {
	base := 1.1 + 0.1*float64(tick%5)
	if tick%20 == 7 {
		base = 4.8
	}
	window := make([]float64, 8)
	for i := range window {
		window[i] = base
	}
	return sample{
		DeviceID: "motor-01",
		Metric:   "vibration_rms",
		Value:    math.Round(rms(window)*100) / 100,
		Level:    levelName(rms(window)),
		TempC:    55 + float64(tick%10),
	}
}

func main() {
	target := flag.String("url", "http://127.0.0.1:8080", "cloud base url")
	ticks := flag.Int("ticks", 30, "number of samples")
	interval := flag.Duration("interval", 500*time.Millisecond, "delay between samples")
	flag.Parse()

	client := &http.Client{Timeout: 3 * time.Second}
	for tick := 0; tick < *ticks; tick++ {
		s := reading(tick)
		body, err := json.Marshal(s)
		if err != nil {
			log.Fatalf("marshal: %v", err)
		}
		resp, err := client.Post(*target+"/api/v1/samples", "application/json", bytes.NewReader(body))
		if err != nil {
			fmt.Printf("tick=%d value=%.2f level=%s cloud=down (%v)\n", tick, s.Value, s.Level, err)
		} else {
			resp.Body.Close()
			fmt.Printf("tick=%d value=%.2f level=%s cloud=%d\n", tick, s.Value, s.Level, resp.StatusCode)
		}
		time.Sleep(*interval)
	}
}
