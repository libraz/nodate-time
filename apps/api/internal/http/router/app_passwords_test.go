package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func serve(h http.Handler, method, path, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// The profile link is opened by a device that is not signed in, so it sits in
// the public group and spends the sign-in budget; issuing one needs a session.
func TestAppPasswordProfileRouteGroups(t *testing.T) {
	t.Parallel()
	h, _ := BuildResult(Deps{Mailer: relayStub{}, PasswordLoginEnabled: true, AuthRateLimit: 2, ShareRateLimit: -1, DAVRateLimit: -1})

	const profile = "/auth/app-password-profiles/abc.def"
	require.Equal(t, http.StatusGone, serve(h, http.MethodGet, profile, ""), "answered without credentials")
	require.Equal(t, http.StatusGone, serve(h, http.MethodGet, profile, ""))
	require.Equal(t, http.StatusTooManyRequests, serve(h, http.MethodGet, profile, ""), "counted against the auth limit")
	require.Equal(t, http.StatusTooManyRequests, serve(h, http.MethodPost, "/auth/login", `{}`),
		"the same budget as sign-in")

	require.Equal(t, http.StatusUnauthorized,
		serve(h, http.MethodPost, "/user/app-passwords/profile-links", `{"name":"iPhone"}`), "issuing needs a session")
}
