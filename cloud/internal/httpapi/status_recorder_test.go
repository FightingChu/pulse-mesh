package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestMetricsMatchFinalHTTPStatus(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		serve  http.HandlerFunc
	}{
		{"explicit then duplicate", http.StatusCreated, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusCreated)
			writer.WriteHeader(http.StatusInternalServerError)
		}},
		{"implicit then duplicate", http.StatusOK, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = io.WriteString(writer, "ok")
			writer.WriteHeader(http.StatusInternalServerError)
		}},
		{"informational then body", http.StatusOK, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusEarlyHints)
			_, _ = io.WriteString(writer, "ok")
		}},
		{"informational then return", http.StatusOK, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusEarlyHints)
		}},
		{"informational then explicit", http.StatusCreated, func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusContinue)
			writer.WriteHeader(http.StatusEarlyHints)
			writer.WriteHeader(http.StatusCreated)
		}},
		{"empty handler", http.StatusOK, func(writer http.ResponseWriter, request *http.Request) {}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			metrics := NewMetrics()
			completed := make(chan struct{})
			handler := withLogging(metrics, scenario.serve)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				defer close(completed)
				handler.ServeHTTP(writer, request)
			}))
			defer server.Close()
			response, err := server.Client().Get(server.URL + "/health")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != scenario.status {
				t.Fatalf("HTTP status = %d, want %d", response.StatusCode, scenario.status)
			}
			<-completed
			key := fmt.Sprintf("GET|/health|%d", response.StatusCode)
			if len(metrics.counts) != 1 || metrics.counts[key] != 1 {
				t.Errorf("metrics %v do not match HTTP status %d", metrics.counts, response.StatusCode)
			}
			scrape := httptest.NewRecorder()
			metrics.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			want := fmt.Sprintf(`pulse_http_requests_total{method="GET",path="/health",status="%d"} 1`, response.StatusCode)
			if !strings.Contains(scrape.Body.String(), want) {
				t.Errorf("missing final-status counter %s in %s", want, scrape.Body.String())
			}
		})
	}
}

func TestStatusRecorderPreservesSwitchingProtocols(t *testing.T) {
	response := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: response}
	recorder.WriteHeader(http.StatusSwitchingProtocols)
	recorder.WriteHeader(http.StatusInternalServerError)
	if recorder.status != response.Code || recorder.status != http.StatusSwitchingProtocols {
		t.Fatalf("recorded=%d response=%d, want 101", recorder.status, response.Code)
	}
}

func TestStatusRecorderDoesNotRecordRejectedHeader(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("invalid status did not panic")
			}
		}()
		recorder.WriteHeader(9999)
	}()
	if recorder.status != 0 {
		t.Fatalf("rejected header changed recorded status to %d", recorder.status)
	}
}
