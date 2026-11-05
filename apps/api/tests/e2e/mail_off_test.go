package e2e

import (
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type authProviders struct {
	MailEnabled bool `json:"mailEnabled"`
}

func TestProvidersReportMailEnabledWhenAMailerIsConfigured(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithMailer(t, testDB, &countingMailer{})
	var out authProviders
	helpers.DoJSON(t, http.MethodGet, srv.BaseURL+"/auth/oauth/providers", "", nil, &out)
	require.True(t, out.MailEnabled)
}

// With mail delivery off, the mail-only operations are not offered and the
// providers response says so; registration and sign-in still work.
func TestMailOffWithdrawsTheMailOnlyFlows(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	srv := helpers.NewTestServerWithMailer(t, testDB, mailer.NoopMailer{})
	var out authProviders
	helpers.DoJSON(t, http.MethodGet, srv.BaseURL+"/auth/oauth/providers", "", nil, &out)
	require.False(t, out.MailEnabled)

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/auth/password-reset/request"},
		{http.MethodPost, "/auth/password-reset/confirm"},
		{http.MethodPost, "/auth/verify-email/confirm"},
		{http.MethodPost, "/user/verify-email/resend"},
	} {
		resp := stackRequest(t, route.method, srv.BaseURL+route.path, nil)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, "%s %s", route.method, route.path)
	}
}
