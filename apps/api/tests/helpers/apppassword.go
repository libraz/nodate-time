package helpers

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/apppasswords"
)

// VerifyEmail marks a registered tenant's email as verified. Registration
// leaves it unverified, and app password creation refuses such an account.
func VerifyEmail(t *testing.T, db *sql.DB, userPublicID string) {
	t.Helper()
	res, err := db.Exec(
		`UPDATE users SET email_verified_at = NOW(3) WHERE public_id = UUID_TO_BIN(?)`,
		userPublicID)
	if err != nil {
		t.Fatalf("verify email: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("verify email: user %s not found", userPublicID)
	}
}

// CreateAppPassword inserts a live app password for a registered tenant
// straight into the table and returns the plaintext a client would present.
// It bypasses the REST route so a test of the DAV surface does not depend on
// it. The row dies with its user.
func CreateAppPassword(t *testing.T, db *sql.DB, userPublicID, name string) string {
	t.Helper()
	plain, prefix, hash, err := apppassword.Generate()
	if err != nil {
		t.Fatalf("generate app password: %v", err)
	}
	ws := TestWorkspace(generated.New(db))
	res, err := db.Exec(
		`INSERT INTO dav_app_passwords (public_id, workspace_id, user_id, name, token_hash, token_prefix)
		 SELECT UUID_TO_BIN(UUID()), ?, id, ?, ?, ? FROM users WHERE public_id = UUID_TO_BIN(?)`,
		ws.ID, name, hash, prefix, userPublicID)
	if err != nil {
		t.Fatalf("insert app password: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("insert app password: user %s not found", userPublicID)
	}
	return plain
}

// MintProfileTicket signs a profile link ticket with the test server's keys,
// for a session and issue time the test chooses, so a test can present a link
// that has expired or names a session that never existed.
func MintProfileTicket(t *testing.T, sessionPub, name string, issuedAt time.Time) string {
	t.Helper()
	session, err := uuid.Parse(sessionPub)
	if err != nil {
		t.Fatalf("parse session id: %v", err)
	}
	claims, err := apppasswords.NewProfileClaims(session, name)
	if err != nil {
		t.Fatalf("profile claims: %v", err)
	}
	ticket, err := apppasswords.NewProfileKeys([]byte(TestJWTSecret)).Mint(claims, issuedAt)
	if err != nil {
		t.Fatalf("mint profile ticket: %v", err)
	}
	return ticket
}
