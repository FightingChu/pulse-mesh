package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

type Metrics struct {
	mu     sync.Mutex
	counts map[string]int
	stores map[string]storeMetric
}

type storeMetric struct {
	records      int
	bytes        int64
	writesFailed uint64
}

func NewMetrics() *Metrics {
	return &Metrics{counts: make(map[string]int), stores: make(map[string]storeMetric)}
}

func (m *Metrics) setStore(name string, records int, bytes int64, failures uint64) {
	m.mu.Lock()
	m.stores[name] = storeMetric{records: records, bytes: bytes, writesFailed: failures}
	m.mu.Unlock()
}

func normalizePath(path string) string {
	switch path {
	case "/health", "/health/live", "/health/ready", "/metrics", "/api/v1/samples", "/api/v1/samples/batch", "/api/v1/alerts", "/api/v1/diagnose", "/api/v1/diagnoses":
		return path
	}
	if strings.HasPrefix(path, "/api/v1/alerts/") && strings.HasSuffix(path, "/ack") {
		return "/api/v1/alerts/{id}/ack"
	}
	return "unmatched"
}

func (m *Metrics) observe(method, path string, status int) {
	key := fmt.Sprintf("%s|%s|%d", method, normalizePath(path), status)
	m.mu.Lock()
	m.counts[key]++
	m.mu.Unlock()
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.observe(r.Method, r.URL.Path, http.StatusOK)
		m.mu.Lock()
		keys := make([]string, 0, len(m.counts))
		snapshot := make(map[string]int, len(m.counts))
		for key, value := range m.counts {
			keys = append(keys, key)
			snapshot[key] = value
		}
		stores := make(map[string]storeMetric, len(m.stores))
		for name, metric := range m.stores {
			stores[name] = metric
		}
		m.mu.Unlock()

		sort.Strings(keys)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintln(w, "# TYPE pulse_http_requests_total counter")
		for _, key := range keys {
			parts := strings.SplitN(key, "|", 3)
			if len(parts) != 3 {
				continue
			}
			fmt.Fprintf(w, "pulse_http_requests_total{method=%q,path=%q,status=%q} %d\n",
				parts[0], parts[1], parts[2], snapshot[key])
		}
		fmt.Fprintln(w, "# TYPE pulse_store_records gauge")
		fmt.Fprintln(w, "# TYPE pulse_store_journal_bytes gauge")
		fmt.Fprintln(w, "# TYPE pulse_store_write_failures_total counter")
		for _, name := range []string{"samples", "diagnoses", "alerts"} {
			metric := stores[name]
			fmt.Fprintf(w, "pulse_store_records{store=%q} %d\n", name, metric.records)
			fmt.Fprintf(w, "pulse_store_journal_bytes{store=%q} %d\n", name, metric.bytes)
			fmt.Fprintf(w, "pulse_store_write_failures_total{store=%q} %d\n", name, metric.writesFailed)
		}
	})
}

func withLogging(metrics *Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		if normalizePath(r.URL.Path) != "/metrics" {
			metrics.observe(r.Method, r.URL.Path, rec.status)
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Microsecond))
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
