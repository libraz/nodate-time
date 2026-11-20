package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The API lower-cases addresses at sign-in, so an account stored with its
// given case could never be signed in to.
func TestCreateUserStoresTheAddressNormalized(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	lower := bootstrapEmail("cu-norm")
	mixed := "  " + upperFirst(lower) + "  "
	out, code := runCreateUser(t, "-email", mixed, "-password", "longenough1")
	require.Zero(t, code, "output: %s", out)

	require.True(t, userExists(t, lower))
	var subject string
	require.NoError(t, testDB.QueryRow(
		`SELECT i.subject FROM identities i JOIN users u ON u.id = i.user_id
		 WHERE u.email = ? AND i.provider = 'local'`, lower).Scan(&subject))
	require.Equal(t, lower, subject)
}

func upperFirst(s string) string {
	return string(s[0]-32) + s[1:]
}
