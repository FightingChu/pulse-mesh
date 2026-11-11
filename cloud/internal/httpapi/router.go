package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"pulse-mesh/cloud/internal/alerts"
	"pulse-mesh/cloud/internal/diagnose"
	"pulse-mesh/cloud/internal/ingest"
)

func New(store *ingest.Store, engine *alerts.Engine, ai *diagnose.Client, diagnoses *diagnose.Store) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/samples", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var s ingest.Sample
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if s.DeviceID == "" || s.Metric == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id and metric are required"})
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if req.DeviceID == "" || len(req.Values) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device_id and values are required"})
			return
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

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
