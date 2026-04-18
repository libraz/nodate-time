package helpers

import (
	"database/sql"
	"testing"
)

// PurgeAll deletes all data from all tables (for test isolation).
func PurgeAll(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := []string{
		"event_comments",
		"calendar_invites",
		"memos",
		"events",
		"calendar_members",
		"calendars",
		"users",
	}
	db.Exec("SET FOREIGN_KEY_CHECKS = 0")
	for _, table := range tables {
		db.Exec("TRUNCATE TABLE " + table)
	}
	db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}
