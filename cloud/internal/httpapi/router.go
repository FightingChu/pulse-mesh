package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/ingest"
)

const maxJSONBody = 128 * 1024
const maxWaveformValues = 1024

func New(store *ingest.Store, engine *alerts.Engine, ai *diagnose.Client, diagnoses *diagnose.Store) http.Handler {
	metrics := NewMetrics()
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/samples", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var s ingest.Sample
			if err := decodeJSON(w, r, &s); err != nil {
				writeDecodeError(w, err)
				return
			}
			if s.DeviceID == "" || s.Metric == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id and metric are required"})
				return
			}
			if !finite(s.Value) || !finite(s.TempC) || (s.Level != "" && s.Level != "normal" && s.Level != "warning" && s.Level != "critical") {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sample values or level"})
				return
			}
			store.Add(s)
			created := engine.Evaluate(s)
			writeJSON(w, http.StatusCreated, map[string]any{
				"stored": store.Count(),
				"alert":  created,
			})
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.List(r.URL.Query().Get("device"), 100))
		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/api/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		includeAcked := r.URL.Query().Get("include_acked") == "1"
		writeJSON(w, http.StatusOK, engine.List(r.URL.Query().Get("device"), includeAcked))
	})

	mux.HandleFunc("/api/v1/alerts/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/"), "/ack")
		if id == "" || !strings.HasSuffix(r.URL.Path, "/ack") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "use POST /api/v1/alerts/{id}/ack"})
			return
		}
		if !engine.Ack(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "alert not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "acked", "id": id})
	})

	mux.HandleFunc("/api/v1/diagnose", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var req diagnose.Request
		if err := decodeJSON(w, r, &req); err != nil {
			writeDecodeError(w, err)
			return
		}
		if req.DeviceID == "" || len(req.Values) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id and values are required"})
			return
		}
		if len(req.Values) != maxWaveformValues {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "values must contain exactly 1024 samples"})
			return
		}
		if !finite(req.SampleRateHz) || !finite(req.RotationHz) || req.SampleRateHz < 2*req.RotationHz || req.RotationHz < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sample_rate_hz or rotation_hz"})
			return
		}
		for _, value := range req.Values {
			if !finite(value) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "waveform values must be finite numbers"})
				return
			}
		}
		if req.SampleRateHz <= 0 {
			req.SampleRateHz = 1000
		}
		if req.RotationHz <= 0 {
			req.RotationHz = 25
		}

		result, err := ai.Diagnose(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		diagnoses.Add(result)
		alert := engine.EvaluateFault(result.DeviceID, result.Fault)
		writeJSON(w, http.StatusOK, map[string]any{
			"diagnosis": result,
			"alert":     alert,
		})
	})

	mux.HandleFunc("/api/v1/diagnoses", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		writeJSON(w, http.StatusOK, diagnoses.List(r.URL.Query().Get("device"), 100))
	})

	return withCORS(withLogging(metrics, withRecovery(mux)))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request must contain a single JSON value")
		}
		return err
	}
	return nil
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
