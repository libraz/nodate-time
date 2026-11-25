package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// eventLabel is the label an event row is drawn under.
type eventLabel struct {
	publicID string
	color    string
}

// resolveLabel maps a label's public id onto its internal id, requiring that
// it is a label of calID. An empty id is no label. Anything else that names no
// label of the calendar is EventLabelUnknown, so a client holding a label
// deleted elsewhere can tell that apart from a malformed request.
func resolveLabel(ctx context.Context, q *generated.Queries, calID uint32, id string) (uint32, *apierrors.Spec) {
	if id == "" {
		return 0, nil
	}
	pub, err := parseUUID(id)
	if err != nil {
		return 0, apierrors.EventLabelUnknown
	}
	l, err := q.GetCalendarLabelByPublicID(ctx, generated.GetCalendarLabelByPublicIDParams{
		PublicID:   pub,
		CalendarID: calID,
	})
	if err != nil {
		return 0, apierrors.EventLabelUnknown
	}
	return l.ID, nil
}

// storedLabel is the internal id of the label rowID is linked to, 0 for none.
func storedLabel(ctx context.Context, q *generated.Queries, rowID uint32) (uint32, error) {
	id, err := q.GetEventLabelID(ctx, rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// setEventLabel links rowID to labelID (0 unlinks it) and advances the row's
// entity tag. It writes nothing when the link already says so, and reports
// whether it changed anything.
func setEventLabel(ctx context.Context, q *generated.Queries, rowID, labelID uint32) (bool, error) {
	current, err := storedLabel(ctx, q, rowID)
	if err != nil || current == labelID {
		return false, err
	}
	if labelID == 0 {
		err = q.DeleteEventLabel(ctx, rowID)
	} else {
		err = q.SetEventLabel(ctx, generated.SetEventLabelParams{EventID: rowID, LabelID: labelID})
	}
	if err != nil {
		return false, err
	}
	// A label-only edit changes no column of the row, so ON UPDATE never fires.
	return true, q.TouchCalendarEvent(ctx, rowID)
}

// applyEventLabel writes the resolved label on the row an edit produced: the
// occurrence's override, else the series (the new one after a split). When a
// whole-series edit changes the label, every override of the series takes it
// too. The rows the response is rendered from are re-read when any changed.
func applyEventLabel(ctx context.Context, q *generated.Queries, edited editedRows, labelID uint32, wholeSeries bool) (editedRows, error) {
	target := edited.series.ID
	if edited.override != nil {
		target = edited.override.ID
	}
	changed, err := setEventLabel(ctx, q, target, labelID)
	if err != nil || !changed {
		return edited, err
	}
	if wholeSeries && edited.override == nil {
		overrides, err := q.ListRecurrenceOverridesByParents(ctx, []sql.NullInt32{{Int32: int32(target), Valid: true}})
		if err != nil {
			return edited, err
		}
		for _, o := range overrides {
			if _, err := setEventLabel(ctx, q, o.ID, labelID); err != nil {
				return edited, err
			}
		}
	}
	series, err := q.GetCalendarEventByID(ctx, edited.series.ID)
	if err != nil {
		return edited, err
	}
	edited.series = series
	if edited.override != nil {
		override, err := q.GetCalendarEventByID(ctx, edited.override.ID)
		if err != nil {
			return edited, err
		}
		edited.override = &override
	}
	return edited, nil
}
