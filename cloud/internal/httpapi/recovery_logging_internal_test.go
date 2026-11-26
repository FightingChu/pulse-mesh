package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingPreservesCommittedStatusWhenHandlerPanics(t *testing.T) {
	metrics := NewMetrics()
	inner := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health" {
			writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		writer.Header().Set("Content-Length", "128")
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, "partial response")
		panic("render failed after response started")
	})
	server := httptest.NewServer(withLogging(metrics, withRecovery(inner)))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/panic", "application/json", nil)
	if err != nil {
		if response != nil {
			response.Body.Close()
		}
		if !errors.Is(err, io.EOF) {
			t.Fatalf("incomplete response error = %v, want EOF", err)
		}
	} else {
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusCreated || string(body) != "partial response" || !errors.Is(readErr, io.ErrUnexpectedEOF) {
			t.Fatalf("status=%d body=%q error=%v; want interrupted 201 response", response.StatusCode, body, readErr)
		}
	}
	if len(metrics.counts) != 1 || metrics.counts[`POST|unmatched|201`] != 1 {
		t.Fatalf("panic request metrics=%v, want original 201", metrics.counts)
	}
	health, err := server.Client().Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	var healthBody map[string]string
	if err := json.NewDecoder(health.Body).Decode(&healthBody); err != nil {
		t.Fatal(err)
	}
	if health.StatusCode != http.StatusOK || healthBody["status"] != "ok" {
		t.Fatalf("server after abort = %d %+v", health.StatusCode, healthBody)
	}
}

func TestLoggingKeepsRecoveryBeforeResponseCommit(t *testing.T) {
	metrics := NewMetrics()
	response := httptest.NewRecorder()
	handler := withLogging(metrics, withRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("render failed before response started")
	})))
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || body["error"] != "internal error" {
		t.Fatalf("recovery response = %d %+v", response.Code, body)
	}
	if len(metrics.counts) != 1 || metrics.counts[`GET|unmatched|500`] != 1 {
		t.Fatalf("recovery metrics=%v, want 500", metrics.counts)
	}
}

func TestLoggingPreservesCommittedStatusOnAbortedPanic(t *testing.T) {
	metrics := NewMetrics()
	response := httptest.NewRecorder()
	handler := withLogging(metrics, withRecovery(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(writer, "partial response")
		panic("handler aborted")
	})))
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/partial", nil))
	}()
	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered panic = %v, want ErrAbortHandler", recovered)
	}
	if response.Code != http.StatusCreated || response.Body.String() != "partial response" {
		t.Fatalf("aborted response = %d %q", response.Code, response.Body.String())
	}
	if len(metrics.counts) != 1 || metrics.counts[`POST|unmatched|201`] != 1 {
		t.Fatalf("metrics %v do not preserve the committed status", metrics.counts)
	}
}
