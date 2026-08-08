package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCreateUserAsksForNoServingConfiguration pins that this command answers
// only the configuration it reads.
//
// It writes a user row. It signs no session, serves no request, stores no
// object and sends no mail, so the guards that describe a serving deployment
// are not its to satisfy. It used to load them anyway and was only startable
// because TC_ENV=development switched all four off at once -- which meant the
// documented way to run it was to turn every guard off, and the way to run it
// in an environment that had lost that exemption was to set a signing secret
// it would never use.
//
// The environment below is deliberately hostile: the published default signing
// secret, no mail relay, the published object-storage credentials, a wildcard
// CORS origin, and no TC_ENV to excuse any of it. A serving process must
// refuse every one of those. This command must not care about any of them.
func TestCreateUserAsksForNoServingConfiguration(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	hostile := []string{
		"TC_ENV=production",
		"TC_JWT_SECRET=dev-secret-change-me-in-production",
		"TC_SMTP_HOST=",
		"TC_S3_ACCESS_KEY=minioadmin",
		"TC_S3_SECRET_KEY=minioadmin",
		"TC_CORS_ALLOWED_ORIGINS=*",
		"TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS=false",
		"TC_ALLOW_CONSOLE_MAILER=false",
	}

	email := fmt.Sprintf("cu-noserving-%d@test.local", time.Now().UnixNano())
	out, code := runCreateUserWithEnv(t, hostile,
		"-email", email, "-password", "password123", "-name", "No Serving")

	require.Zero(t, code, "the command reads none of these; output: %s", out)
	require.True(t, userExists(t, email), "the account should have been created")
}

// TestCreateUserStillRefusesADatabaseItCannotReach is the other half: dropping
// the serving guards must not drop the one piece of configuration this command
// does use. A DSN that cannot be parsed has to stop it before it reports
// success against nothing.
func TestCreateUserStillRefusesADatabaseItCannotReach(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("cu-baddsn-%d@test.local", time.Now().UnixNano())
	out, code := runCreateUserWithEnv(t, []string{"TC_DB_DSN=not a dsn"},
		"-email", email, "-password", "password123")

	require.NotZero(t, code, "an unparseable DSN must stop the command; output: %s", out)
	require.False(t, userExists(t, email), "a refused run must leave no account")
}
