package httpapi

import (
	"encoding/json"
	"net/http"

	"pulse-mesh/cloud/internal/ingest"
)

func New(store *ingest.Store) http.Handler {
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
			writeJSON(w, http.StatusCreated, map[string]int{"stored": store.Count()})
		case http.MethodGet:
			writeJSON(w, http.StatusOK, store.List(r.URL.Query().Get("device"), 100))
		default:
			w.Header().Set("Allow", "GET, POST")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		}
	})

	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
