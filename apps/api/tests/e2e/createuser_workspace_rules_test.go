package e2e

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A zone the API would refuse is refused here too, before any row is written:
// stored, it is read in the browser as a calendar drawn in the wrong place.
func TestCreateUserRefusesAnUnknownTimezone(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	for _, tc := range []struct {
		name string
		env  []string
		args []string
	}{
		{"flag", nil, []string{"-timezone", "Mars/Olympus"}},
		{"workspace default", []string{"TC_WORKSPACE_TIMEZONE=Mars/Olympus"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			email := bootstrapEmail("cu-badtz")
			out, code := runCreateUserWithEnv(t, tc.env,
				append([]string{"-email", email, "-password", "password123"}, tc.args...)...)
			require.NotZero(t, code, "output: %s", out)
			require.Contains(t, out, "IANA timezone")
			require.False(t, userExists(t, email), "nothing is written for a refused zone")
		})
	}
}

// The command can be what first creates the workspace; when it is, the row
// carries the configured country, as it would had the API created it.
func TestCreateUserCreatesTheWorkspaceWithTheConfiguredCountry(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	slug := fmt.Sprintf("cu-country-%d", time.Now().UnixNano())
	out, code := runCreateUserWithEnv(t, []string{"TC_WORKSPACE_SLUG=" + slug, "TC_WORKSPACE_COUNTRY=US"},
		"-email", bootstrapEmail("cu-country"), "-password", "password123")
	require.Zero(t, code, "output: %s", out)

	var country sql.NullString
	require.NoError(t, testDB.QueryRow(`SELECT country FROM workspaces WHERE slug = ?`, slug).Scan(&country))
	require.Equal(t, "US", country.String)
}
