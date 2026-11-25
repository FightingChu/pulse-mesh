package httpapi

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAccessLogEscapesRequestFields(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	for _, target := range []string{"/health", "/device%0Aforged%0D%09%1B%00", "/%22quoted%22/%E7%94%B5%E6%9C%BA"} {
		t.Run(target, func(t *testing.T) {
			output.Reset()
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := httptest.NewRecorder()
			handler := withLogging(NewMetrics(), http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}))
			handler.ServeHTTP(response, request)
			text := output.String()
			if strings.Count(text, "\n") != 1 || strings.ContainsAny(text, "\r\t\x1b\x00") {
				t.Errorf("request fields broke the log record: %q", text)
			}
			want := strconv.Quote(request.Method) + " " + strconv.Quote(request.URL.Path) + " 204 "
			if !strings.Contains(text, want) {
				t.Errorf("missing escaped request fields %q in %q", want, text)
			}
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", response.Code)
			}
		})
	}
}

func TestRecoveryLogEscapesPanicFieldsAndKeepsStack(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	request := httptest.NewRequest(http.MethodGet, "/fault%0Aforged-path%1B", nil)
	response := httptest.NewRecorder()
	message := "failure\nforged-error\r\t\x1b\x00"
	withRecovery(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		panic(message)
	})).ServeHTTP(response, request)
	text := output.String()
	firstLine, stack, ok := strings.Cut(text, "\n")
	want := "panic " + strconv.Quote(request.Method) + " " + strconv.Quote(request.URL.Path) + ": " + strconv.Quote(message)
	if !ok || !strings.Contains(firstLine, want) || strings.ContainsAny(firstLine, "\r\t\x1b\x00") {
		t.Errorf("panic fields broke the log header: %q", firstLine)
	}
	if !strings.Contains(stack, "goroutine ") || !strings.Contains(stack, "runtime/debug.Stack") {
		t.Errorf("panic stack was lost: %q", stack)
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || payload["error"] != "internal error" {
		t.Fatalf("unexpected recovery response: %d %+v", response.Code, payload)
	}
}
