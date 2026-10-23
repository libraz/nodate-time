package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestRegisterRefusesAnAddressTheColumnCannotHold is the front-door half of
// the same defect the OAuth callback had.
//
// users.email is latin1 and documented as ASCII only. Registration looked the
// address up before deciding anything, MySQL refused the comparison, and the
// caller was told the server had failed. Unlike the OAuth path this needed no
// configuration to reach: the lookup runs on every deployment, so an
// internationalised address met a 500 at the front door of every install.
//
// Huma's format:"email" does not reject non-ASCII, so nothing stopped it
// before the handler.
func TestRegisterRefusesAnAddressTheColumnCannotHold(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("%s-%d@example.com", "田中", time.Now().UnixNano())

	status, body := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Charset", "email": email, "password": "password123"})

	require.Equal(t, http.StatusBadRequest, status,
		"an address this deployment cannot store is the caller's input, not a server fault: %s", string(body))
	require.Contains(t, string(body), "AUTH.EMAIL_UNSUPPORTED",
		"and the refusal should name the address as the problem")
	require.NotContains(t, string(body), "INTERNAL.UNEXPECTED")
}

// TestRegisterStillAcceptsAnOrdinaryAddress is the other half: refusing what
// the column cannot hold must not refuse what it can. It drives the same guard
// on the way to a successful registration.
func TestRegisterStillAcceptsAnOrdinaryAddress(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("charset-ok-%d@example.com", time.Now().UnixNano())

	var out struct {
		Token string `json:"token"`
		User  struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Charset OK", "email": email, "password": "password123"}, &out)

	require.NotEmpty(t, out.Token)
	require.Equal(t, email, out.User.Email)
}

// Every other door that looks an address up gives the answer it gives any
// address it has no account for, rather than letting the charset error out
// as a server fault.
func TestAddressLookupsAnswerAnUnstorableAddressNormally(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("%s-%d@example.com", "田中", time.Now().UnixNano())

	t.Run("login", func(t *testing.T) {
		status, body := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
			map[string]any{"email": email, "password": "password123"})
		require.Equal(t, http.StatusUnauthorized, status, string(body))
		require.Contains(t, string(body), "AUTH.BAD_CREDENTIALS")
	})

	t.Run("password reset request", func(t *testing.T) {
		var out struct {
			OK bool `json:"ok"`
		}
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/password-reset/request", "",
			map[string]any{"email": email}, &out)
		require.True(t, out.OK, "the reset answer is uniform whatever the address")
	})

	t.Run("dev login", func(t *testing.T) {
		queries := generated.New(testDB)
		srv := httptest.NewServer(app.NewHandler(router.Deps{
			DB:                   testDB,
			Queries:              queries,
			WorkspaceID:          helpers.TestWorkspace(queries).ID,
			JWTSecret:            helpers.TestJWTSecret,
			Mailer:               testMailer,
			WebURL:               helpers.TestWebURL,
			PasswordLoginEnabled: true,
			AuthRateLimit:        -1,
			DevMode:              true,
		}, app.Options{}))
		t.Cleanup(srv.Close)

		status, body := helpers.DoJSONStatus(t, http.MethodPost, srv.URL+"/auth/dev-login", "",
			map[string]any{"email": email})
		require.Equal(t, http.StatusUnauthorized, status, string(body))
		require.Contains(t, string(body), "AUTH.BAD_CREDENTIALS")
	})
}
