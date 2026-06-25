// Package audit records calendar mutation history into the audit_log table.
package audit

import (
	"context"
	"database/sql"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Entity types recorded in the audit log.
const (
	EntityEvent = "event"
	EntityMemo  = "memo"
)

// Actions recorded in the audit log.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// summaryMaxRunes bounds the stored summary so an oversized title cannot exceed
// the column width; it is truncated on a rune boundary to avoid splitting a
// multi-byte character.
const summaryMaxRunes = 500

// Record appends one audit-log entry. Errors are intentionally swallowed: audit
// logging must never fail or block the user-facing mutation it accompanies.
func Record(ctx context.Context, q *generated.Queries, calendarID, entityID uint32, entityPublicID []byte, entityType, action string, actorID uint32, summary string) {
	if q == nil {
		return
	}
	_ = q.InsertAuditLog(ctx, generated.InsertAuditLogParams{
		CalendarID:     calendarID,
		EntityType:     entityType,
		EntityID:       entityID,
		EntityPublicID: entityPublicID,
		Action:         action,
		ActorID:        sql.NullInt32{Int32: int32(actorID), Valid: actorID != 0},
		Summary:        truncateRunes(summary, summaryMaxRunes),
	})
}

// truncateRunes returns s limited to at most max runes.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
