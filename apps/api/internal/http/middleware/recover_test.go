package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecovererReturnsTheErrorEnvelope(t *testing.T) {
	var nilMap map[string]string
	h := Recoverer()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// The shape a real handler panics with: a write to a map nobody made.
		nilMap["key"] = "value"
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/calendars", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body struct {
		Status  int    `json:"status"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not the error envelope: %v (%s)", err, rec.Body.String())
	}
	if body.Code != "INTERNAL.UNEXPECTED" {
		t.Errorf("code = %q, want INTERNAL.UNEXPECTED", body.Code)
	}
	if body.Status != http.StatusInternalServerError {
		t.Errorf("body status = %d, want 500", body.Status)
	}
}

func TestRecovererLetsAHealthyHandlerThrough(t *testing.T) {
	h := Recoverer()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
}

// http.ErrAbortHandler is how a handler says "stop, without a response". It has
// to reach net/http rather than being turned into a 500.
func TestRecovererRepanicsAbortHandler(t *testing.T) {
	h := Recoverer()(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler to propagate", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("expected the abort to propagate")
}
