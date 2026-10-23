package helpers

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// With no *testing.T the caller can only exit non-zero, so the cause and the
// way to fix it have to be on stderr before OpenTestDB gives up.
func TestOpenTestDBReportsAnUnreachableDatabaseWithoutATest(t *testing.T) {
	t.Setenv("TC_DB_PORT", "1")

	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stderr
	os.Stderr = w
	db := OpenTestDB(nil)
	os.Stderr = orig
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	require.Nil(t, db)
	require.Contains(t, string(out), "test database not available")
	require.Contains(t, string(out), "docker compose up -d mysql")
}
