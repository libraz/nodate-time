package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// A refusal written by middleware is a declared Spec, byte for byte the shape
// a handler's refusal has, so the client-message check covers its code.
func TestMiddlewareRefusalsAreDeclaredSpecs(t *testing.T) {
	never := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("reached the handler") })
	for _, tc := range []struct {
		name string
		h    http.Handler
		want *apierrors.Spec
	}{
		{"no authorization header", RequireAuth("secret", nil)(never), apierrors.AuthTokenMissing},
		{"not a bearer token", RequireAuth("secret", nil)(never), apierrors.AuthTokenInvalid},
		{"admin gate without an actor", RequireAdmin(nil)(never), apierrors.AuthTokenInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if tc.name == "not a bearer token" {
				req.Header.Set("Authorization", "Basic abc")
			}
			rec := httptest.NewRecorder()
			tc.h.ServeHTTP(rec, req)
			var got apierrors.HumaError
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body is not the error envelope: %s", rec.Body.String())
			}
			want := apierrors.HumaError{Status: tc.want.Status, Code: tc.want.Code, Message: tc.want.Message}
			if rec.Code != tc.want.Status || got != want {
				t.Fatalf("got %d %+v, want %+v", rec.Code, got, want)
			}
		})
	}
}
