package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

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
