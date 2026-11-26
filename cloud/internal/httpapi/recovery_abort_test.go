package httpapi

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoveryPropagatesAbortWithoutWritingOrLogging(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	for _, prefix := range []string{"", "partial response"} {
		t.Run(prefix, func(t *testing.T) {
			output.Reset()
			response := httptest.NewRecorder()
			handler := withRecovery(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if prefix != "" {
					writer.Header().Set("Content-Length", "128")
					_, _ = io.WriteString(writer, prefix)
				}
				panic(http.ErrAbortHandler)
			}))
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/abort", nil))
			}()
			if recovered != http.ErrAbortHandler {
				t.Errorf("abort panic = %v, want ErrAbortHandler", recovered)
			}
			if response.Body.String() != prefix || response.Header().Get("Content-Type") == "application/json" {
				t.Errorf("recovery changed aborted response: headers=%v body=%q", response.Header(), response.Body.String())
			}
			if output.Len() != 0 {
				t.Errorf("intentional abort generated a panic log: %s", output.String())
			}
		})
	}
}

func TestRecoveryAbortInterruptsHTTPResponse(t *testing.T) {
	for _, prefix := range []string{"", "partial response"} {
		t.Run(prefix, func(t *testing.T) {
			server := httptest.NewServer(withRecovery(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/health" {
					writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
					return
				}
				if prefix != "" {
					_, _ = io.WriteString(writer, prefix)
					writer.(http.Flusher).Flush()
				}
				panic(http.ErrAbortHandler)
			})))
			defer server.Close()
			response, err := server.Client().Post(server.URL+"/abort", "application/json", nil)
			if prefix == "" {
				if response != nil {
					response.Body.Close()
				}
				if err == nil {
					t.Fatal("aborted request returned a complete HTTP response")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				response.Body.Close()
				if !errors.Is(readErr, io.ErrUnexpectedEOF) || string(body) != prefix {
					t.Fatalf("aborted body = %q, error=%v; want unchanged prefix and interrupted response", body, readErr)
				}
			}
			health, err := server.Client().Get(server.URL + "/health")
			if err != nil {
				t.Fatal(err)
			}
			defer health.Body.Close()
			if health.StatusCode != http.StatusOK {
				t.Fatalf("server unavailable after abort: status=%d", health.StatusCode)
			}
		})
	}
}
