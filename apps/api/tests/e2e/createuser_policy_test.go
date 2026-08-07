package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The bootstrap CLI writes accounts the API then has to live with, so what it
// accepts has to be what the API accepts -- in both directions.

func TestCreateUserPasswordPolicyAgreesWithTheAPI(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	t.Run("three characters are three characters, not nine bytes", func(t *testing.T) {
		const password = "あいう"
		require.Len(t, []rune(password), 3)
		require.Len(t, password, 9)

		email := bootstrapEmail("cu-runes-short")
		out, code := runCreateUser(t, "-email", email, "-password", password)
		require.NotZero(t, code, "output: %s", out)
		require.Contains(t, out, "password must be at least 8 characters")
		require.False(t, userExists(t, email),
			"a password the API would refuse must not become an account here either")
	})

	t.Run("the maximum applies here too", func(t *testing.T) {
		password := strings.Repeat("a", 200)
		email := bootstrapEmail("cu-runes-long")
		out, code := runCreateUser(t, "-email", email, "-password", password)
		require.NotZero(t, code, "output: %s", out)
		require.Contains(t, out, "at most 128 characters")
		require.False(t, userExists(t, email))
	})

	t.Run("eight characters are enough however many bytes they take", func(t *testing.T) {
		const password = "あいうえおかきく"
		require.Len(t, []rune(password), 8)

		email := bootstrapEmail("cu-runes-ok")
		out, code := runCreateUser(t, "-email", email, "-password", password)
		require.Zero(t, code, "output: %s", out)

		status, raw := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/register", "",
			map[string]any{"name": "Accepted", "email": bootstrapEmail("cu-api-ok"), "password": password})
		require.Equal(t, http.StatusOK, status,
			"the API accepts the same password: %s", string(raw))

		status, _ = helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
			map[string]any{"email": email, "password": password})
		require.Equal(t, http.StatusOK, status)
	})
}

// -admin with -skip-existing, against an address that already has an ordinary
// account, cannot grant anything: -skip-existing says the account is not this
// command's to change. What it must not do is exit zero, because a bootstrap
// script reads that as "the administrator you asked for exists" when what
// exists is an ordinary user.
func TestCreateUserFailsWhenTheAdminItWasAskedForWasNotGranted(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := bootstrapEmail("cu-not-granted")
	const original = "correcthorsebattery"
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Existing", "email": email, "password": original}, nil)
	require.False(t, isInstanceAdmin(t, email))

	out, code := runCreateUser(t, "-email", email, "-password", "a-different-password",
		"-admin", "-skip-existing")
	require.NotZero(t, code, "output: %s", out)
	require.Contains(t, out, "skipped")
	require.Contains(t, out, "admin",
		"the run has to name the rights it was asked for and did not give")

	require.False(t, isInstanceAdmin(t, email),
		"an existing account is still not promoted by a flag combination meant for seeding")

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": original})
	require.Equal(t, http.StatusOK, status,
		"and the password given on the command line must not have replaced theirs")
}

// Re-seeding is the case the flag pair is for: the account exists and already
// holds the grant, so nothing was withheld and the run succeeds.
func TestCreateUserSkipsQuietlyWhenTheAdminAlreadyHoldsTheGrant(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := bootstrapEmail("cu-reseed")
	out, code := runCreateUser(t, "-email", email, "-password", "correcthorsebattery",
		"-admin", "-skip-existing")
	require.Zero(t, code, "output: %s", out)
	require.True(t, isInstanceAdmin(t, email))

	out, code = runCreateUser(t, "-email", email, "-password", "correcthorsebattery",
		"-admin", "-skip-existing")
	require.Zero(t, code, "output: %s", out)
	require.Contains(t, out, "skipped")
	require.True(t, isInstanceAdmin(t, email))
}
