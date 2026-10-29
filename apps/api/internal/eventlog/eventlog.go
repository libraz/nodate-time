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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Event types. The names are the registry's: sql/core/event-kinds.yaml for
// kinds on core tables and sql/time/event-kinds.yaml for this product's own.
// They are dotted and consumers match on prefix, so adding a kind never
// requires a consumer to change. TestRegistry holds this block, kinds below
// and both yaml files to the same set.
const (
	TypeCalendarSetUp               = "calendar.created"
	TypeCalendarEdited              = "calendar.updated"
	TypeCalendarGone                = "calendar.deleted"
	TypeCalendarSubscribed          = "calendar.subscribed"
	TypeCalendarSubscriptionUpdated = "calendar.subscription.updated"

	TypeMemberAdded    = "calendar.member.added"
	TypeMemberRemoved  = "calendar.member.removed"
	TypeMemberRoleSet  = "calendar.member.role_changed"
	TypeMemberColorSet = "calendar.member.updated"

	TypeEventCreated = "calendar.event.created"
	TypeEventUpdated = "calendar.event.updated"
	TypeEventDeleted = "calendar.event.deleted"

	// What people do to an event after it exists is most of what happens on a
	// shared calendar. Without these the history of an event reads as though
	// nobody ever discussed it, ticked anything off, or attached anything.
	TypeCommentCreated    = "calendar.event.comment.created"
	TypeCommentUpdated    = "calendar.event.comment.updated"
	TypeCommentDeleted    = "calendar.event.comment.deleted"
	TypeChecklistCreated  = "calendar.event.checklist.created"
	TypeChecklistUpdated  = "calendar.event.checklist.updated"
	TypeChecklistDeleted  = "calendar.event.checklist.deleted"
	TypeAttachmentCreated = "calendar.event.attachment.created"
	TypeAttachmentDeleted = "calendar.event.attachment.deleted"
	TypeAttendeeAdded     = "calendar.event.attendee.added"
	TypeAttendeeRemoved   = "calendar.event.attendee.removed"
	// TypeAttendeeUpdated is an attendee's edit grant changing. A grant decides
	// who may rewrite the event, so it is an access change and belongs in the
	// history as much as the edits it permits. An answer is TypeRSVPUpdated.
	TypeAttendeeUpdated    = "calendar.event.attendee.updated"
	TypeRSVPUpdated        = "calendar.event.rsvp.updated"
	TypeEventInviteCreated = "calendar.event.invite.created"
	TypeEventInviteRotated = "calendar.event.invite.rotated"
	TypeEventInviteRevoked = "calendar.event.invite.revoked"

	TypeMemoCreated   = "calendar.memo.created"
	TypeMemoUpdated   = "calendar.memo.updated"
	TypeMemoCompleted = "calendar.memo.completed"
	TypeMemoDeleted   = "calendar.memo.deleted"

	TypeReminder = "calendar.reminder"

	TypeWorkspaceMemberAdded       = "workspace.member.added"
	TypeWorkspaceMemberRemoved     = "workspace.member.removed"
	TypeWorkspaceMemberRoleChanged = "workspace.member.role_changed"

	// Kinds only this product writes (sql/time/event-kinds.yaml).
	TypeInviteCreated = "calendar.invite.created"
	TypeInviteRevoked = "calendar.invite.revoked"
	TypePhotoUploaded = "calendar.photo.uploaded"
	TypePhotoUpdated  = "calendar.photo.updated"
	TypePhotoDeleted  = "calendar.photo.deleted"
)

// Tables a kind's subject can name, and the payload key each one's public id
// is also written under.
const (
	subjectEvent  = "calendar_events"
	subjectMemo   = "calendar_memos"
	subjectPhoto  = "album_photos"
	subjectInvite = "calendar_invites"
)

var subjectPayloadKey = map[string]string{
	subjectEvent:  "eventId",
	subjectMemo:   "memoId",
	subjectPhoto:  "photoId",
	subjectInvite: "inviteId",
}

// kinds maps every kind to the table its subject_public_id names, empty when
// the column stays NULL. It is the registries' subject column, held here so
// Append can refuse a row the registries would not allow.
var kinds = map[string]string{
	TypeCalendarSetUp:               "",
	TypeCalendarEdited:              "",
	TypeCalendarGone:                "",
	TypeCalendarSubscribed:          "",
	TypeCalendarSubscriptionUpdated: "",
	TypeMemberAdded:                 "",
	TypeMemberRemoved:               "",
	TypeMemberRoleSet:               "",
	TypeMemberColorSet:              "",
	TypeEventCreated:                subjectEvent,
	TypeEventUpdated:                subjectEvent,
	TypeEventDeleted:                subjectEvent,
	TypeCommentCreated:              subjectEvent,
	TypeCommentUpdated:              subjectEvent,
	TypeCommentDeleted:              subjectEvent,
	TypeChecklistCreated:            subjectEvent,
	TypeChecklistUpdated:            subjectEvent,
	TypeChecklistDeleted:            subjectEvent,
	TypeAttachmentCreated:           subjectEvent,
	TypeAttachmentDeleted:           subjectEvent,
	TypeAttendeeAdded:               subjectEvent,
	TypeAttendeeRemoved:             subjectEvent,
	TypeAttendeeUpdated:             subjectEvent,
	TypeRSVPUpdated:                 subjectEvent,
	TypeEventInviteCreated:          subjectEvent,
	TypeEventInviteRotated:          subjectEvent,
	TypeEventInviteRevoked:          subjectEvent,
	TypeMemoCreated:                 subjectMemo,
	TypeMemoUpdated:                 subjectMemo,
	TypeMemoCompleted:               subjectMemo,
	TypeMemoDeleted:                 subjectMemo,
	TypeReminder:                    "",
	TypeWorkspaceMemberAdded:        "",
	TypeWorkspaceMemberRemoved:      "",
	TypeWorkspaceMemberRoleChanged:  "",
	TypeInviteCreated:               subjectInvite,
	TypeInviteRevoked:               subjectInvite,
	TypePhotoUploaded:               subjectPhoto,
	TypePhotoUpdated:                subjectPhoto,
	TypePhotoDeleted:                subjectPhoto,
}

// labelKeys names the payload key the registry gives Entry.Summary on the
// kinds whose label is not a free-text summary.
var labelKeys = map[string]string{
	TypeChecklistCreated:  "itemTitle",
	TypeChecklistUpdated:  "itemTitle",
	TypeChecklistDeleted:  "itemTitle",
	TypeAttachmentCreated: "filename",
	TypeAttachmentDeleted: "filename",
}

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
	// Title is the event's title, written as payload "title" on every kind
	// whose subject is an event.
	Title string
	// Summary is the human-readable label of what changed: payload "summary",
	// or the key the registry names for the kind (itemTitle, filename).
	Summary string
	// Subject is the public id of the entity the kind's registry subject
	// names, as it appears in API responses, and is written to
	// events.subject_public_id. It is required for a kind with a subject and
	// refused for one without. The log stores public ids only: an internal id
	// in a payload would leak the sequence the API exists to hide.
	Subject []byte
	// Extra carries the keys the registry requires beyond the subject's own
	// (calendarId, userId, commentId, ...) and any type-specific ones. A
	// consumer that does not recognise a key must preserve it, so nothing
	// here is a closed set.
	Extra map[string]any
}

// UUID renders a public id the way payloads carry it.
func UUID(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
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

	subjectTable, known := kinds[e.Type]
	if !known {
		return fmt.Errorf("eventlog: %q is not a registered kind", e.Type)
	}
	switch {
	case subjectTable != "" && len(e.Subject) != 16:
		return fmt.Errorf("eventlog: entry %q needs the public id of a %s row as its subject", e.Type, subjectTable)
	case subjectTable == "" && len(e.Subject) > 0:
		return fmt.Errorf("eventlog: entry %q has no subject in the registry", e.Type)
	}

	payload := make(map[string]any, len(e.Extra)+4)
	for k, v := range e.Extra {
		payload[k] = v
	}
	if subjectTable != "" {
		payload[subjectPayloadKey[subjectTable]] = UUID(e.Subject)
	}
	if subjectTable == subjectEvent {
		payload["title"] = truncateRunes(e.Title, summaryMaxRunes)
	}
	if e.Summary != "" {
		key := labelKeys[e.Type]
		if key == "" {
			key = "summary"
		}
		payload[key] = truncateRunes(e.Summary, summaryMaxRunes)
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
		PublicID:        pubID[:],
		WorkspaceID:     e.WorkspaceID,
		CalendarID:      nullID(e.CalendarID),
		ActorUserID:     nullID(e.ActorUserID),
		SubjectPublicID: e.Subject,
		Type:            e.Type,
		PayloadJSON:     body,
		OccurredAt:      time.Now().UTC(),
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
