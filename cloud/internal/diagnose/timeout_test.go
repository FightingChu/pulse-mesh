package diagnose_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"pulse-mesh/cloud/internal/diagnose"
)

func TestAIClientTimeoutTripsCircuit(t *testing.T) {
	for _, duringBody := range []bool{false, true} {
		name := "response headers"
		if duringBody {
			name = "response body"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, request.Body)
				if duringBody {
					writer.Header().Set("Content-Type", "application/json")
					_, _ = writer.Write([]byte(`{"device_id":`))
					writer.(http.Flusher).Flush()
				}
				select {
				case <-request.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			client := diagnose.NewClientWithBreaker(server.URL, 100*time.Millisecond, 1, time.Minute)
			callerContext := context.Background()
			request := diagnose.Request{DeviceID: "motor-01", Values: []float64{1}}
			if _, err := client.Diagnose(callerContext, request); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout error = %v, want DeadlineExceeded", err)
			}
			if callerContext.Err() != nil {
				t.Fatal("AI timeout changed caller context")
			}
			if _, err := client.Diagnose(callerContext, request); !errors.Is(err, diagnose.ErrCircuitOpen) {
				t.Fatalf("second request error = %v, want circuit open", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("AI calls = %d, want 1", calls.Load())
			}
		})
	}
}

func TestCallerDeadlineDoesNotTripCircuit(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		if calls.Add(1) == 1 {
			select {
			case <-request.Context().Done():
			case <-release:
			}
			return
		}
		_ = json.NewEncoder(writer).Encode(diagnose.Result{DeviceID: "motor-01", Fault: "normal"})
	}))
	defer server.Close()
	defer close(release)
	client := diagnose.NewClientWithBreaker(server.URL, 5*time.Second, 1, time.Minute)
	callerContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request := diagnose.Request{DeviceID: "motor-01", Values: []float64{1}}
	if _, err := client.Diagnose(callerContext, request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller timeout error = %v", err)
	}
	if _, err := client.Diagnose(context.Background(), request); err != nil {
		t.Fatalf("healthy request after caller timeout: %v", err)
	}
}
