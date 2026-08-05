// Package eventlog appends to the shared event log.
//
// Every state change writes exactly one row here, in the same transaction
// as the rows it describes. That is what makes the change visible to
// anything else on the database: notification fan-out, realtime delivery
// and the activity feed all read this log rather than polling tables.
//
// Unlike the audit table it replaces, an append that fails is not
// swallowed. A change that lands without its event is a change no other
// process can see, so the caller must let the error roll the transaction
// back -- a silently missing row is worse than a rejected write, because
// nothing downstream can tell it apart from nothing having happened.
package eventlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Event types. The names are dotted and consumers match on prefix, so
// adding a kind never requires a consumer to change.
const (
	TypeEventCreated   = "calendar.event.created"
	TypeEventUpdated   = "calendar.event.updated"
	TypeEventDeleted   = "calendar.event.deleted"
	TypeMemoCreated    = "calendar.memo.created"
	TypeMemoUpdated    = "calendar.memo.updated"
	TypeMemoDeleted    = "calendar.memo.deleted"
	TypeMemberJoined   = "calendar.member.joined"
	TypeMemberLeft     = "calendar.member.left"
	TypeMemberRemoved  = "calendar.member.removed"
	TypeMemberRoleSet  = "calendar.member.role_changed"
	TypeInviteCreated  = "calendar.invite.created"
	TypeInviteRevoked  = "calendar.invite.revoked"
	TypePhotoUploaded  = "calendar.photo.uploaded"
	TypePhotoUpdated   = "calendar.photo.updated"
	TypePhotoDeleted   = "calendar.photo.deleted"
	TypeCalendarSetUp  = "calendar.created"
	TypeCalendarEdited = "calendar.updated"
	TypeCalendarGone   = "calendar.deleted"
	// What people do to an event after it exists is most of what happens on a
	// shared calendar. Without these the history of an event reads as though
	// nobody ever discussed it, ticked anything off, or attached anything.
	TypeCommentAdded    = "calendar.comment.added"
	TypeCommentEdited   = "calendar.comment.edited"
	TypeCommentRemoved  = "calendar.comment.removed"
	TypeChecklistAdded  = "calendar.checklist.added"
	TypeChecklistSet    = "calendar.checklist.updated"
	TypeChecklistGone   = "calendar.checklist.removed"
	TypeAttachmentAdded = "calendar.attachment.added"
	TypeAttachmentGone  = "calendar.attachment.removed"
)

// summaryMaxRunes bounds the stored summary. It is cut on a rune boundary
// so a multi-byte character is never split in half.
const summaryMaxRunes = 500

// Entry is one state change.
type Entry struct {
	WorkspaceID uint32
	// CalendarID is the calendar the change is about. Zero means the change
	// belongs to no calendar in particular and the column is left NULL.
	CalendarID uint32
	// ActorUserID is the acting user. Zero means a system action.
	ActorUserID uint32
	Type        string
	// Summary is the human-readable line the activity feed shows.
	Summary string
	// Subject is the public id of the row that changed, as it appears in
	// API responses. The log stores public ids only: an internal id in a
	// payload would leak the sequence the API exists to hide.
	Subject []byte
	// Extra carries type-specific keys. A consumer that does not recognise
	// a key must preserve it, so nothing here is a closed set.
	Extra map[string]any
}

// Append writes the entry. q must be bound to the same transaction as the
// change being recorded; passing the pool-wide Queries would let the change
// commit while the event rolls back, which is the exact split this exists
// to prevent.
func Append(ctx context.Context, q *generated.Queries, e Entry) error {
	if e.Type == "" {
		return fmt.Errorf("eventlog: entry has no type")
	}
	if e.WorkspaceID == 0 {
		return fmt.Errorf("eventlog: entry %q has no workspace", e.Type)
	}

	payload := make(map[string]any, len(e.Extra)+2)
	for k, v := range e.Extra {
		payload[k] = v
	}
	if len(e.Subject) > 0 {
		if u, err := uuid.FromBytes(e.Subject); err == nil {
			payload["id"] = u.String()
		}
	}
	if e.Summary != "" {
		payload["summary"] = truncateRunes(e.Summary, summaryMaxRunes)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("eventlog: marshal payload for %q: %w", e.Type, err)
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("eventlog: generate id for %q: %w", e.Type, err)
	}

	if _, err := q.AppendEvent(ctx, generated.AppendEventParams{
		PublicID:    pubID[:],
		WorkspaceID: e.WorkspaceID,
		CalendarID:  nullID(e.CalendarID),
		ActorUserID: nullID(e.ActorUserID),
		Type:        e.Type,
		PayloadJSON: body,
		OccurredAt:  time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("eventlog: append %q: %w", e.Type, err)
	}
	return nil
}

func nullID(id uint32) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(id), Valid: id != 0}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
