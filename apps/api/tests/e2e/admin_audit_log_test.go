package e2e

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// auditRows reads the instance audit rows one actor wrote for one action:
// how many there are, and the payloads joined for a look at what they carry.
func auditRows(t *testing.T, db rowQuerier, action, actorPublicID string) (count int, payloads string) {
	t.Helper()
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*), COALESCE(GROUP_CONCAT(CAST(l.payload_json AS CHAR) SEPARATOR ' '), '')
		   FROM instance_audit_logs l
		   JOIN users u ON u.id = l.actor_user_id
		  WHERE l.action = ? AND u.public_id = UUID_TO_BIN(?)`,
		action, actorPublicID).Scan(&count, &payloads))
	return count, payloads
}

// Every instance-wide change an administrator makes leaves exactly one audit
// row in the transaction that made it, and the OAuth secret is not in it.
func TestAdminOAuthWritesAppendOneAuditRowWithoutTheSecret(t *testing.T) {
	bootstrap(t)

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	tx, err := testDB.Begin()
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.Exec(`DELETE FROM oauth_provider_configs WHERE provider IN ('google', 'line')`)
	require.NoError(t, err)
	srv := newTxAdminServer(t, tx, users.OAuthConfig{})

	const secret = "super-secret-value-that-must-not-be-logged"
	putProvider(t, srv.URL, admin.AccessToken, "line",
		map[string]any{"clientId": "audited-line-client", "clientSecret": secret, "enabled": true})
	count, payloads := auditRows(t, tx, "oauth_provider.update", admin.UserID)
	require.Equal(t, 1, count, "one save, one audit row")
	require.NotContains(t, payloads, secret)
	require.Contains(t, payloads, "audited-line-client")

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, srv.URL+"/admin/oauth-providers/line", admin.AccessToken, nil)
	require.Less(t, status, 300, string(raw))
	count, _ = auditRows(t, tx, "oauth_provider.delete", admin.UserID)
	require.Equal(t, 1, count)

	// A delete that removed nothing changed nothing, so it records nothing.
	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, srv.URL+"/admin/oauth-providers/line", admin.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	count, _ = auditRows(t, tx, "oauth_provider.delete", admin.UserID)
	require.Equal(t, 1, count, "a miss writes no audit row")
}

func TestAdminAllowedEmailWritesAppendOneAuditRow(t *testing.T) {
	bootstrap(t)
	// A live allow-list row restricts the whole instance: serial phase only.

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	address := fmt.Sprintf("audited-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() { withdrawAllowedEmail(t, address) })
	var created struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": address, "reason": "audit"}, &created)
	count, _ := auditRows(t, testDB, "allowed_email.create", admin.UserID)
	require.Equal(t, 1, count)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/admin/allowed-emails/"+created.ID, admin.AccessToken, nil)
	require.Less(t, status, 300, string(raw))
	count, _ = auditRows(t, testDB, "allowed_email.delete", admin.UserID)
	require.Equal(t, 1, count)

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/admin/allowed-emails/"+created.ID, admin.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	count, _ = auditRows(t, testDB, "allowed_email.delete", admin.UserID)
	require.Equal(t, 1, count, "withdrawing an address twice is logged once")
}

func TestAdminRevokeAppendsOneAuditRow(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	keeper := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, keeper.UserID)
	helpers.MakeInstanceAdmin(t, testDB, leaver.UserID)

	status, raw := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+leaver.UserID, keeper.AccessToken, nil)
	require.Less(t, status, 300, string(raw))
	count, _ := auditRows(t, testDB, "instance_admin.revoke", keeper.UserID)
	require.Equal(t, 1, count)

	status, _ = helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+leaver.UserID, keeper.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status)
	count, _ = auditRows(t, testDB, "instance_admin.revoke", keeper.UserID)
	require.Equal(t, 1, count, "revoking an already revoked administrator is logged once")
}

// A member's colour change is a change to the calendar, so it is in the
// calendar's log, written with the change.
func TestMemberColourChangeAppendsOneEvent(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tt := helpers.NewTenant(t, testServerURL)
	status, raw := helpers.DoJSONStatus(t, http.MethodPut,
		testServerURL+"/calendars/"+tt.CalendarID+"/members/"+tt.UserID+"/color", tt.AccessToken,
		map[string]any{"color": "#F35F8C"})
	require.Less(t, status, 300, string(raw))

	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM events e JOIN calendars c ON c.id = e.calendar_id
		  WHERE c.public_id = UUID_TO_BIN(?) AND e.type = 'calendar.member.updated'`,
		tt.CalendarID).Scan(&n))
	require.Equal(t, 1, n)
}
