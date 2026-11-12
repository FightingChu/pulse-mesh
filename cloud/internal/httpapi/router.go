package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/ingest"
	"pulse-mesh/cloud/internal/journal"
)

const maxJSONBody = 128 * 1024
const maxWaveformValues = 1024
const maxConcurrentDiagnoses = 4
const maxSampleBatch = 100

func New(store *ingest.Store, engine *alerts.Engine, ai *diagnose.Client, diagnoses *diagnose.Store) http.Handler {
	metrics := NewMetrics()
	mux := http.NewServeMux()
	diagnoseSlots := make(chan struct{}, maxConcurrentDiagnoses)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		sampleCount, sampleBytes, sampleFailures := store.Stats()
		diagnosisCount, diagnosisBytes, diagnosisFailures := diagnoses.Stats()
		alertCount, alertBytes, alertFailures := engine.Stats()
		metrics.setStore("samples", sampleCount, sampleBytes, sampleFailures)
		metrics.setStore("diagnoses", diagnosisCount, diagnosisBytes, diagnosisFailures)
		metrics.setStore("alerts", alertCount, alertBytes, alertFailures)
		metrics.Handler().ServeHTTP(w, r)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		checks := map[string]error{
			"samples":   store.Check(),
			"diagnoses": diagnoses.Check(),
			"alerts":    engine.Check(),
		}
		for name, err := range checks {
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "store": name})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	mux.HandleFunc("/api/v1/samples", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var s ingest.Sample
			if err := decodeJSON(w, r, &s); err != nil {
				writeDecodeError(w, err)
				return
			}
			if err := validateSample(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			added, created, err := processSample(store, engine, s)
			if err != nil {
				writeStorageError(w, err, "sample persistence failed")
				return
			}
			if !added {
				writeJSON(w, http.StatusOK, map[string]any{
					"stored":    store.Count(),
					"duplicate": true,
					"alert":     created,
				})
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{
				"stored": store.Count(),
				"alert":  created,
			})
		case http.MethodGet:
			limit, err := queryLimit(r, 100)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, store.List(r.URL.Query().Get("device"), limit))
		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/api/v1/samples/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		var samples []ingest.Sample
		if err := decodeJSON(w, r, &samples); err != nil {
			writeDecodeError(w, err)
			return
		}
		if len(samples) == 0 || len(samples) > maxSampleBatch {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "batch must contain between 1 and 100 samples"})
			return
		}
		for index, sample := range samples {
			if err := validateSample(sample); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "index": index})
				return
			}
		}
		addedItems, batchAlerts, err := processSamples(store, engine, samples)
		if err != nil {
			writeStorageError(w, err, "batch persistence failed")
			return
		}
		stored := 0
		duplicates := 0
		for _, added := range addedItems {
			if added {
				stored++
			} else {
				duplicates++
			}
		}
		status := http.StatusCreated
		if stored == 0 {
			status = http.StatusOK
		}
		writeJSON(w, status, map[string]any{
			"received":   len(samples),
			"stored":     stored,
			"duplicates": duplicates,
			"alerts":     batchAlerts,
		})
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
		acked, err := engine.AckWithError(id)
		if err != nil {
			writeStorageError(w, err, "alert persistence failed")
			return
		}
		if !acked {
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
		if len(req.DeviceID) > 64 || len(req.EventID) > 128 || strings.ContainsAny(req.DeviceID+req.EventID, "\r\n\x00") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id and event_id exceed limits or contain control characters"})
			return
		}
		if len(req.Values) != maxWaveformValues {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "values must contain exactly 1024 samples"})
			return
		}
		if !finite(req.SampleRateHz) || !finite(req.RotationHz) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sample_rate_hz or rotation_hz"})
			return
		}
		if req.SampleRateHz == 0 {
			req.SampleRateHz = 1000
		}
		if req.RotationHz == 0 {
			req.RotationHz = 25
		}
		if req.SampleRateHz < 240 || req.RotationHz < 0 || req.SampleRateHz < 2*req.RotationHz {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sample_rate_hz must be at least 240 and satisfy the rotation Nyquist limit"})
			return
		}
		for _, value := range req.Values {
			if !finite(value) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "waveform values must be finite numbers"})
				return
			}
		}
		if req.EventID != "" {
			if previous, ok := diagnoses.Get(req.DeviceID, req.EventID); ok {
				alert, err := engine.EvaluateFaultEventWithError(previous.DeviceID, previous.EventID, previous.Fault)
				if err != nil {
					writeStorageError(w, err, "alert persistence failed")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"diagnosis": previous,
					"alert":     alert,
					"duplicate": true,
				})
				return
			}
		}
		select {
		case diagnoseSlots <- struct{}{}:
			defer func() { <-diagnoseSlots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "diagnosis capacity reached"})
			return
		}

		result, err := ai.Diagnose(r.Context(), req)
		if err != nil {
			if errors.Is(err, diagnose.ErrCircuitOpen) {
				w.Header().Set("Retry-After", "15")
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		added, err := diagnoses.Add(result)
		if err != nil {
			writeStorageError(w, err, "diagnosis persistence failed")
			return
		}
		if !added {
			if previous, ok := diagnoses.Get(req.DeviceID, req.EventID); ok {
				alert, err := engine.EvaluateFaultEventWithError(previous.DeviceID, previous.EventID, previous.Fault)
				if err != nil {
					writeStorageError(w, err, "alert persistence failed")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"diagnosis": previous,
					"alert":     alert,
					"duplicate": true,
				})
				return
			}
		}
		alert, err := engine.EvaluateFaultEventWithError(result.DeviceID, result.EventID, result.Fault)
		if err != nil {
			writeStorageError(w, err, "alert persistence failed")
			return
		}
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
		limit, err := queryLimit(r, 100)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, diagnoses.List(r.URL.Query().Get("device"), limit))
	})

	return withCORS(withLogging(metrics, withRecovery(mux)))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeStorageError(w http.ResponseWriter, err error, message string) {
	status := http.StatusInternalServerError
	if errors.Is(err, journal.ErrCapacity) {
		status = http.StatusInsufficientStorage
	}
	writeJSON(w, status, map[string]string{"error": message})
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

func validateSample(s ingest.Sample) error {
	if s.DeviceID == "" || s.Metric == "" {
		return errors.New("device_id and metric are required")
	}
	if len(s.DeviceID) > 64 || len(s.Metric) > 64 || len(s.EventID) > 128 {
		return errors.New("device_id and metric must be at most 64 characters; event_id must be at most 128 characters")
	}
	if strings.ContainsAny(s.DeviceID+s.Metric+s.EventID, "\r\n\x00") {
		return errors.New("sample identifiers must not contain control characters")
	}
	if !finite(s.Value) || !finite(s.TempC) || (s.Level != "" && s.Level != "normal" && s.Level != "warning" && s.Level != "critical") {
		return errors.New("invalid sample values or level")
	}
	return nil
}

func processSample(store *ingest.Store, engine *alerts.Engine, sample ingest.Sample) (bool, *alerts.Alert, error) {
	addedItems, createdAlerts, err := processSamples(store, engine, []ingest.Sample{sample})
	if err != nil {
		return false, nil, err
	}
	if len(createdAlerts) == 0 {
		return addedItems[0], nil, nil
	}
	return addedItems[0], &createdAlerts[0], nil
}

func processSamples(store *ingest.Store, engine *alerts.Engine, samples []ingest.Sample) ([]bool, []alerts.Alert, error) {
	added, err := store.AddBatch(samples)
	if err != nil {
		return nil, nil, err
	}
	created, err := engine.EvaluateBatchWithError(samples)
	if err != nil {
		return added, nil, err
	}
	return added, created, nil
}

func queryLimit(r *http.Request, defaultLimit int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("limit must be an integer between 1 and 100")
	}
	return limit, nil
}
