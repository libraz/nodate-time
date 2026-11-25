package calendars

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

// Bounds on a calendar's labels. The cap keeps the label picker of an event
// to a row of chips that still fits on a phone.
const (
	maxLabelsPerCalendar = 50
	maxLabelNameRunes    = 50
)

// labelETag names one revision of a label by its last write.
func labelETag(l generated.CalendarLabel) string {
	stamp := l.CreatedAt
	if l.UpdatedAt.Valid {
		stamp = l.UpdatedAt.Time
	}
	return `"` + stamp.UTC().Format("20060102T150405.000") + `"`
}

func mapLabel(l generated.CalendarLabel) LabelResponse {
	return LabelResponse{
		ID:    pubIDToHex(l.PublicID),
		Name:  l.Name,
		Color: l.Color,
		ETag:  labelETag(l),
	}
}

// labelName is the stored form of a submitted name. The name is trimmed
// first, so two names differing only in surrounding blanks cannot both exist.
func labelName(raw string) (string, *apierrors.Spec) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxLabelNameRunes {
		return "", apierrors.BadRequest
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", apierrors.BadRequest
	}
	return name, nil
}

// nameTaken maps the unique key on (calendar_id, name) to its error.
func nameTaken(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return apierrors.LabelNameTaken
	}
	return err
}

// lockLabel reads a label of the calendar under its row lock. An id from
// another calendar is not found.
func lockLabel(ctx context.Context, q *generated.Queries, calID uint32, labelID string) (generated.CalendarLabel, error) {
	pub, err := parseUUID(labelID)
	if err != nil {
		return generated.CalendarLabel{}, apierrors.LabelNotFound
	}
	label, err := q.GetCalendarLabelForUpdate(ctx, generated.GetCalendarLabelForUpdateParams{
		PublicID:   pub,
		CalendarID: calID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarLabel{}, apierrors.LabelNotFound
	}
	return label, err
}

// checkLabelIfMatch compares the caller's If-Match with the label's current tag.
func checkLabelIfMatch(header precondition.IfMatch, l generated.CalendarLabel) error {
	err := header.Check(labelETag(l))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, precondition.ErrStale):
		return apierrors.LabelStale
	case errors.Is(err, precondition.ErrMalformed):
		return apierrors.BadIfMatch
	}
	return err
}

func logLabel(ctx context.Context, q *generated.Queries, deps Deps, cal generated.Calendar, actorID uint32, kind string, l generated.CalendarLabel) error {
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  cal.ID,
		ActorUserID: actorID,
		Type:        kind,
		Summary:     l.Name,
		Subject:     l.PublicID,
		Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
	})
}

// ListLabels returns a calendar's labels in creation order.
func ListLabels(deps Deps) func(context.Context, *ListLabelsInput) (*ListLabelsOutput, error) {
	return func(ctx context.Context, in *ListLabelsInput) (*ListLabelsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		rows, err := deps.Queries.ListCalendarLabels(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		labels := make([]LabelResponse, len(rows))
		for i, l := range rows {
			labels[i] = mapLabel(l)
		}
		return &ListLabelsOutput{Body: labels}, nil
	}
}

func CreateLabel(deps Deps) func(context.Context, *CreateLabelInput) (*CreateLabelOutput, error) {
	return func(ctx context.Context, in *CreateLabelInput) (*CreateLabelOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		name, spec := labelName(in.Body.Name)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		cal, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		var created generated.CalendarLabel
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockManage(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			count, err := q.CountCalendarLabels(ctx, cal.ID)
			if err != nil {
				return err
			}
			if count >= maxLabelsPerCalendar {
				return apierrors.LabelLimitReached
			}
			if err := q.CreateCalendarLabel(ctx, generated.CreateCalendarLabelParams{
				PublicID:    pubID[:],
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				Name:        name,
				Color:       strings.ToUpper(in.Body.Color),
			}); err != nil {
				return nameTaken(err)
			}
			created, err = q.GetCalendarLabelByPublicID(ctx, generated.GetCalendarLabelByPublicIDParams{
				PublicID:   pubID[:],
				CalendarID: cal.ID,
			})
			if err != nil {
				return err
			}
			return logLabel(ctx, q, deps, cal, userID, eventlog.TypeLabelCreated, created)
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &CreateLabelOutput{ETag: labelETag(created), Body: mapLabel(created)}, nil
	}
}

// UpdateLabel renames or recolours a label. A patch that leaves both values
// as stored writes nothing and logs nothing.
func UpdateLabel(deps Deps) func(context.Context, *UpdateLabelInput) (*UpdateLabelOutput, error) {
	return func(ctx context.Context, in *UpdateLabelInput) (*UpdateLabelOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		if in.Body.Name.IsNull() || in.Body.Color.IsNull() {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		var name, color string
		if v, ok := in.Body.Name.Get(); ok {
			var spec *apierrors.Spec
			if name, spec = labelName(v); spec != nil {
				return nil, apierrors.ToHuma(spec)
			}
		}
		if v, ok := in.Body.Color.Get(); ok {
			color = strings.ToUpper(v)
		}
		cal, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		var updated generated.CalendarLabel
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockManage(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			current, err := lockLabel(ctx, q, cal.ID, in.LabelID)
			if err != nil {
				return err
			}
			if err := checkLabelIfMatch(in.IfMatch, current); err != nil {
				return err
			}
			newName, newColor := cmp.Or(name, current.Name), cmp.Or(color, current.Color)
			updated = current
			if newName == current.Name && newColor == current.Color {
				return nil
			}
			if err := q.UpdateCalendarLabel(ctx, generated.UpdateCalendarLabelParams{
				Name: newName, Color: newColor, ID: current.ID,
			}); err != nil {
				return nameTaken(err)
			}
			if updated, err = q.GetCalendarLabelByPublicID(ctx, generated.GetCalendarLabelByPublicIDParams{
				PublicID:   current.PublicID,
				CalendarID: cal.ID,
			}); err != nil {
				return err
			}
			return logLabel(ctx, q, deps, cal, userID, eventlog.TypeLabelUpdated, updated)
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &UpdateLabelOutput{ETag: labelETag(updated), Body: mapLabel(updated)}, nil
	}
}

// DeleteLabel removes a label. The events filed under it keep existing and
// fall back to their owner's colour.
func DeleteLabel(deps Deps) func(context.Context, *DeleteLabelInput) (*DeleteLabelOutput, error) {
	return func(ctx context.Context, in *DeleteLabelInput) (*DeleteLabelOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockManage(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			current, err := lockLabel(ctx, q, cal.ID, in.LabelID)
			if err != nil {
				return err
			}
			if err := checkLabelIfMatch(in.IfMatch, current); err != nil {
				return err
			}
			if err := q.DeleteCalendarLabel(ctx, current.ID); err != nil {
				return err
			}
			return logLabel(ctx, q, deps, cal, userID, eventlog.TypeLabelDeleted, current)
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &DeleteLabelOutput{}, nil
	}
}
