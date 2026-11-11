package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoveryReturnsJSON500(t *testing.T) {
	handler := withRecovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "internal error" {
		t.Fatalf("body = %v", body)
	}
}

func TestNormalizePathCollapsesAlertIDs(t *testing.T) {
	cases := map[string]string{
		"/health":                    "/health",
		"/api/v1/alerts/alert-1/ack": "/api/v1/alerts/{id}/ack",
		"/api/v1/alerts":             "/api/v1/alerts",
	}
	for input, want := range cases {
		if got := normalizePath(input); got != want {
			t.Fatalf("normalizePath(%q) = %q, want %q", input, got, want)
		}
	}
}
