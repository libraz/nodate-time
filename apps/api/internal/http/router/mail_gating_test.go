package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/stretchr/testify/require"
)

// relayStub is a mailer that is not the noop one, so it counts as configured.
type relayStub struct{}

func (relayStub) Send(context.Context, mailer.Message) error { return nil }

// mailOnlyRoutes are the operations whose only channel is mail.
var mailOnlyRoutes = []struct{ method, path string }{
	{"post", "/auth/password-reset/request"},
	{"post", "/auth/password-reset/confirm"},
	{"post", "/auth/verify-email/confirm"},
	{"post", "/user/verify-email/resend"},
}

func documentedPaths(t *testing.T, m mailer.Mailer) map[string]map[string]json.RawMessage {
	t.Helper()
	h := Build(Deps{Mailer: m, PasswordLoginEnabled: true, AuthRateLimit: -1, ShareRateLimit: -1})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	return doc.Paths
}

func TestMailOnlyRoutesAreAbsentWithoutAMailer(t *testing.T) {
	t.Parallel()
	for name, m := range map[string]mailer.Mailer{
		"nil mailer":      nil,
		"noop mailer":     mailer.NoopMailer{},
		"dispatched noop": mailer.NewDispatcher(mailer.NoopMailer{}, mailer.DispatcherOptions{}),
	} {
		t.Run(name, func(t *testing.T) {
			paths := documentedPaths(t, m)
			for _, r := range mailOnlyRoutes {
				require.NotContains(t, paths[r.path], r.method, "%s %s must not be offered", r.method, r.path)
			}
			// Sign-in itself does not depend on mail.
			require.Contains(t, paths, "/auth/login")
			require.Contains(t, paths, "/auth/register")
		})
	}
}

func TestMailOnlyRoutesArePresentWithAMailer(t *testing.T) {
	t.Parallel()
	for name, m := range map[string]mailer.Mailer{
		"relay":            relayStub{},
		"dispatched relay": mailer.NewDispatcher(relayStub{}, mailer.DispatcherOptions{}),
	} {
		t.Run(name, func(t *testing.T) {
			paths := documentedPaths(t, m)
			for _, r := range mailOnlyRoutes {
				require.Contains(t, paths[r.path], r.method, "%s %s must be offered", r.method, r.path)
			}
		})
	}
}
