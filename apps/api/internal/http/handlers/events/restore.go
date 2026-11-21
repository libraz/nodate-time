package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

// restoreGrace is how long after a deletion it can still be undone.
const restoreGrace = 10 * time.Minute

type RestoreEventInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}

type RestoreEventOutput struct {
	ETag string `header:"ETag"`
	Body EventResponse
}

type UndoExceptionInput struct {
	CalendarID     string               `path:"calendarId"`
	EventID        string               `path:"eventId"`
	OccurrenceDate string               `path:"occurrenceDate" pattern:"^\\d{4}-\\d{2}-\\d{2}$" doc:"The cancelled occurrence's date, YYYY-MM-DD, in the series' own zone"`
	IfMatch        precondition.IfMatch `header:"If-Match" required:"true"`
}

type UndoExceptionOutput struct{}

// RestoreEvent brings back an event or series deleted within the grace
// period, with the attachments that deletion retired.
func RestoreEvent(deps Deps) func(context.Context, *RestoreEventInput) (*RestoreEventOutput, error) {
	return func(ctx context.Context, in *RestoreEventInput) (*RestoreEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if parent, _ := parseCompositeID(in.EventID); parent != "" {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		pub, err := parseUUID(in.EventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		found, err := deps.Queries.GetCalendarEventAnyStateByPublicID(ctx, generated.GetCalendarEventAnyStateByPublicIDParams{
			WorkspaceID: deps.WorkspaceID,
			PublicID:    pub,
		})
		if err != nil || found.CalendarID != cal.ID || !found.StartAt.Valid || !found.EndAt.Valid {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		var decision eventacl.Decision
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			member, err := calresolve.LockMember(ctx, q, cal.ID, userID)
			if err != nil {
				return err
			}
			evt, err := q.GetCalendarEventAnyStateForUpdate(ctx, found.ID)
			if err != nil {
				return err
			}
			w := eventAccess{cal: cal, member: member, evt: evt, row: evt}
			decision, err = decideForWrite(ctx, q, w, userID)
			if err != nil {
				return err
			}
			if !decision.Exists {
				return apierrors.EventNotFound
			}
			// Restoring takes back what the deletion needed: a whole series
			// was deleted by someone allowed to delete a whole series.
			need := editFields
			if evt.RecurrenceRule != nil {
				need = editGrants
			}
			if err := requireEventEdit(ctx, q, w, userID, need); err != nil {
				return err
			}
			if evt.TaskID.Valid {
				return apierrors.EventTaskOwned
			}
			if evt.Enabled {
				return apierrors.EventNotDeleted
			}
			return restoreDeleted(ctx, q, deps, userID, w)
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to restore event", "eventID", found.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		row, err := deps.Queries.GetCalendarEventByID(ctx, found.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to read restored event", "eventID", found.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		resp, err := editedRows{series: row}.render(ctx, deps, cal)
		if err != nil {
			slog.ErrorContext(ctx, "failed to render restored event", "eventID", found.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		withhold(&resp, decision)
		return &RestoreEventOutput{ETag: resp.Etag, Body: resp}, nil
	}
}

// restoreDeleted undoes the deletion the log still holds for w.evt. w must
// come from the transaction q is bound to.
func restoreDeleted(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess) error {
	deletion, err := q.FindRestorableDeletion(ctx, generated.FindRestorableDeletionParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(w.cal.ID), Valid: true},
		SubjectID:   w.evt.PublicID,
	})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && time.Since(deletion.OccurredAt) > restoreGrace) {
		return apierrors.EventRestoreExpired
	}
	if err != nil {
		return err
	}

	// A re-import may have claimed the UID while the event was deleted.
	if w.evt.SourceUid.Valid {
		other, err := q.FindCalendarEventBySourceUID(ctx, generated.FindCalendarEventBySourceUIDParams{
			CalendarID: w.cal.ID,
			SourceUid:  w.evt.SourceUid,
		})
		switch {
		case err == nil && other.ID != w.evt.ID:
			return apierrors.EventRestoreConflict
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return err
		}
	}
	if err := q.RestoreCalendarEvent(ctx, w.evt.ID); err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return apierrors.EventRestoreConflict
		}
		return err
	}

	// Only the attachments this deletion retired come back; an event deleted
	// by another writer names none.
	var payload struct {
		AttachmentIDs []string `json:"attachmentIds"`
	}
	if err := json.Unmarshal(deletion.PayloadJSON, &payload); err != nil {
		return err
	}
	ids := make([][]byte, 0, len(payload.AttachmentIDs))
	for _, s := range payload.AttachmentIDs {
		if id, err := parseUUID(s); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		if err := q.RestoreAttachmentsByPublicID(ctx, generated.RestoreAttachmentsByPublicIDParams{
			EventID:   sql.NullInt32{Int32: int32(w.evt.ID), Valid: true},
			PublicIds: ids,
		}); err != nil {
			return err
		}
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  w.cal.ID,
		ActorUserID: actorID,
		Type:        eventlog.TypeEventRestored,
		Title:       w.evt.Title,
		Subject:     w.evt.PublicID,
	})
}

// UndoException puts a cancelled occurrence back by removing every spelling of
// its day from the series' exclusion list.
func UndoException(deps Deps) func(context.Context, *UndoExceptionInput) (*UndoExceptionOutput, error) {
	return func(ctx context.Context, in *UndoExceptionInput) (*UndoExceptionOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		day, spec := parseDate(in.OccurrenceDate)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, "", scopeAll)
			if err != nil {
				return err
			}
			d, err := decideForWrite(ctx, q, w, userID)
			if err != nil {
				return err
			}
			if !d.Exists {
				return apierrors.EventNotFound
			}
			if err := requireEventEdit(ctx, q, w, userID, editFields); err != nil {
				return err
			}
			if err := checkTag(in.IfMatch, w); err != nil {
				return err
			}
			if w.evt.RecurrenceRule == nil {
				return apierrors.EventNotRecurring
			}
			rule, err := recurrence.ParseRule(*w.evt.RecurrenceRule)
			if err != nil || rule == nil {
				return apierrors.EventNotRecurring
			}
			occ, err := occurrenceForDate(rule, w.evt, day.Format("20060102"))
			if err != nil {
				return apierrors.EventExceptionNotFound
			}
			current := recurrence.ParseExceptionList(w.evt.RecurrenceExceptions)
			remaining := current.Without(occ.StartAt, w.evt.AllDay, w.evt.Timezone)
			if len(remaining) == len(current) {
				return apierrors.EventExceptionNotFound
			}
			column, err := remaining.MarshalColumn()
			if err != nil {
				return err
			}
			if err := q.SetCancelledOccurrences(ctx, generated.SetCancelledOccurrencesParams{
				RecurrenceExceptions: column,
				ID:                   w.evt.ID,
			}); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeEventUpdated,
				Title:       w.evt.Title,
				Subject:     w.evt.PublicID,
				Extra: map[string]any{
					"scope":         "occurrence",
					"originalStart": occ.StartAt.Format(time.RFC3339),
				},
			})
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to undo occurrence exception", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UndoExceptionOutput{}, nil
	}
}
