package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// The bootstrap CLI writes accounts straight into the database, which means it
// answers to none of the rules the API enforces and nobody sees what it did
// except whoever ran it. These tests run the real command against the test
// database and compare what it accepts with what /auth/register accepts.

// runCreateUser executes the command and returns its combined output and exit
// code. The workspace is the suite's own, so an account it creates is one the
// API would also see.
func runCreateUser(t *testing.T, args ...string) (string, int) {
	t.Helper()
	return runCreateUserWithEnv(t, nil, args...)
}

// runCreateUserWithEnv is runCreateUser with entries appended to the child's
// environment, for the cases where what is under test is the configuration the
// command demands rather than what it writes.
func runCreateUserWithEnv(t *testing.T, extraEnv []string, args ...string) (string, int) {
	t.Helper()

	port := os.Getenv("TC_DB_PORT")
	if port == "" {
		port = "33306"
	}
	name := os.Getenv("TC_DB_NAME")
	if name == "" {
		name = "nodate_time"
	}

	if i := slices.Index(args, "-email"); i >= 0 && i+1 < len(args) && slices.Contains(args, "-admin") {
		revokeInstanceAdminOnCleanup(t, args[i+1])
	}

	cmd := exec.Command("go", append([]string{"run", "../../cmd/createuser"}, args...)...)
	// The database and the workspace are the whole of what this command reads,
	// so they are the whole of what it is given. It used to need TC_ENV as
	// well, to reach an exemption that turned off guards it never used.
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("TC_DB_DSN=%s@tcp(127.0.0.1:%s)/%s?parseTime=true", helpers.TestDBCredentials(), port, name),
		"TC_WORKSPACE_SLUG="+helpers.TestWorkspaceSlug,
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	var exit *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &exit); ok {
			code = exit.ExitCode()
		} else {
			t.Fatalf("run createuser: %v (%s)", err, string(out))
		}
	}
	return string(out), code
}

func asExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func userExists(t *testing.T, email string) bool {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&n))
	return n > 0
}

func isInstanceAdmin(t *testing.T, email string) bool {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM instance_admins ia
		 JOIN users u ON u.id = ia.user_id
		 WHERE u.email = ? AND ia.revoked_at IS NULL AND ia.enabled = TRUE`, email).Scan(&n))
	return n > 0
}

// revokeInstanceAdminOnCleanup revokes the grant an -admin run makes once the
// test ends, so the shared database does not fill up to the admin-list cap.
func revokeInstanceAdminOnCleanup(t *testing.T, email string) {
	t.Cleanup(func() {
		if _, err := testDB.Exec(
			`UPDATE instance_admins ia JOIN users u ON u.id = ia.user_id
			    SET ia.revoked_at = NOW(3)
			  WHERE u.email = ? AND ia.revoked_at IS NULL`, email); err != nil {
			t.Errorf("revoke instance admin: %v", err)
		}
	})
}

func bootstrapEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@test.local", prefix, time.Now().UnixNano())
}

func TestCreateUserRefusesAPasswordShorterThanTheMinimum(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := bootstrapEmail("cu-short")
	out, code := runCreateUser(t, "-email", email, "-password", "abcdefg")
	require.NotZero(t, code, "output: %s", out)
	require.Contains(t, out, "password must be at least 8 characters")
	require.False(t, userExists(t, email), "a refused password must leave no account")
}

// Without the seeding flag the duplicate is an error, and the admin grant that
// shares its transaction goes with it.
func TestCreateUserRefusesADuplicateAndGrantsNothing(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := bootstrapEmail("cu-duplicate")
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Duplicate", "email": email, "password": "correcthorsebattery"}, nil)

	out, code := runCreateUser(t, "-email", email, "-password", "correcthorsebattery", "-admin")
	require.NotZero(t, code, "output: %s", out)
	require.False(t, isInstanceAdmin(t, email),
		"a failed run must not leave the admin grant behind")
}

// The case the flags are for: an address with no account yet is created and
// granted in one transaction.
func TestCreateUserGrantsAdminWhenItActuallyCreatesTheAccount(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := bootstrapEmail("cu-admin")
	out, code := runCreateUser(t, "-email", email, "-password", "correcthorsebattery",
		"-admin", "-skip-existing")
	require.Zero(t, code, "output: %s", out)
	require.Contains(t, out, "role=admin")
	require.True(t, isInstanceAdmin(t, email))

	status, _ := helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": "correcthorsebattery"})
	require.Equal(t, http.StatusOK, status, "and the account it made can sign in")
}
