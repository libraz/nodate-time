package calendars

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

type Deps struct {
	DB      *sql.DB
	Queries *generated.Queries
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	b := u[:]
	return b, nil
}

func mapCalendar(c generated.Calendar) CalendarResponse {
	return CalendarResponse{
		ID:        pubIDToHex(c.PublicID),
		Name:      c.Name,
		Color:     c.Color,
		CoverURL:  c.CoverUrl,
		CreatedAt: c.CreatedAt,
	}
}

// resolveCalendar converts public UUID to internal calendar row + verifies membership.
func resolveCalendar(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	pubBytes, err := parseUUID(calendarPubID)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, apierrors.InternalUnexpected
	}
	// Check membership
	_, err = deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	})
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarAccessDenied
	}
	return cal, nil
}

func ListCalendars(deps Deps) func(context.Context, *ListCalendarsInput) (*ListCalendarsOutput, error) {
	return func(ctx context.Context, _ *ListCalendarsInput) (*ListCalendarsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		rows, err := deps.Queries.ListCalendarsByUser(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		out := &ListCalendarsOutput{Body: make([]CalendarResponse, 0, len(rows))}
		for _, c := range rows {
			out.Body = append(out.Body, mapCalendar(c))
		}
		return out, nil
	}
}

func GetCalendar(deps Deps) func(context.Context, *GetCalendarInput) (*GetCalendarOutput, error) {
	return func(ctx context.Context, in *GetCalendarInput) (*GetCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &GetCalendarOutput{Body: mapCalendar(cal)}, nil
	}
}

func CreateCalendar(deps Deps) func(context.Context, *CreateCalendarInput) (*CreateCalendarOutput, error) {
	return func(ctx context.Context, in *CreateCalendarInput) (*CreateCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		pubID, _ := uuid.NewV7()

		color := in.Body.Color
		if color == "" {
			color = "#4CAF50"
		}

		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		qtx := deps.Queries.WithTx(tx)

		result, err := qtx.CreateCalendar(ctx, generated.CreateCalendarParams{
			PublicID:  pubID[:],
			Name:      in.Body.Name,
			Color:     color,
			CreatedBy: userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		calID, _ := result.LastInsertId()

		// Auto-add creator as admin
		_, err = qtx.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
			CalendarID: uint32(calID),
			UserID:     userID,
			Role:       "admin",
			Color:      color,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &CreateCalendarOutput{}
		out.Body = CalendarResponse{
			ID:        pubID.String(),
			Name:      in.Body.Name,
			Color:     color,
			CreatedAt: time.Now(),
		}
		return out, nil
	}
}

func UpdateCalendar(deps Deps) func(context.Context, *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
	return func(ctx context.Context, in *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		err = deps.Queries.UpdateCalendar(ctx, generated.UpdateCalendarParams{
			Name:     in.Body.Name,
			Color:    in.Body.Color,
			CoverUrl: in.Body.CoverURL,
			ID:       cal.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		updated, _ := deps.Queries.GetCalendarByPublicID(ctx, cal.PublicID)
		return &UpdateCalendarOutput{Body: mapCalendar(updated)}, nil
	}
}

func DeleteCalendar(deps Deps) func(context.Context, *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
	return func(ctx context.Context, in *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// Check admin role
		member, _ := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
			CalendarID: cal.ID,
			UserID:     userID,
		})
		if member.Role != "admin" {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		err = deps.Queries.DeleteCalendar(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteCalendarOutput{}, nil
	}
}

func ListMembers(deps Deps) func(context.Context, *ListMembersInput) (*ListMembersOutput, error) {
	return func(ctx context.Context, in *ListMembersInput) (*ListMembersOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		rows, err := deps.Queries.ListCalendarMembers(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListMembersOutput{Body: make([]MemberResponse, 0, len(rows))}
		for _, m := range rows {
			out.Body = append(out.Body, MemberResponse{
				ID:    pubIDToHex(nil), // User public_id not in join — use user_id as string for now
				Name:  m.UserName,
				Email: m.UserEmail,
				Icon:  m.UserIcon,
				Role:  string(m.Role),
				Color: m.Color,
			})
		}
		return out, nil
	}
}

func ListLabels(_ Deps) func(context.Context, *ListLabelsInput) (*ListLabelsOutput, error) {
	// TimeTree labels are predefined colors
	labels := []LabelResponse{
		{ID: "1", Name: "ラベル1", Color: "#47B2F7"},
		{ID: "2", Name: "ラベル2", Color: "#F35F8C"},
		{ID: "3", Name: "ラベル3", Color: "#B38BDC"},
		{ID: "4", Name: "ラベル4", Color: "#FDC02D"},
		{ID: "5", Name: "ラベル5", Color: "#E73B3B"},
		{ID: "6", Name: "ラベル6", Color: "#2ECC87"},
		{ID: "7", Name: "ラベル7", Color: "#F5A623"},
		{ID: "8", Name: "ラベル8", Color: "#8F8F8F"},
		{ID: "9", Name: "ラベル9", Color: "#42A5F5"},
		{ID: "10", Name: "ラベル10", Color: "#FF7043"},
	}
	return func(_ context.Context, _ *ListLabelsInput) (*ListLabelsOutput, error) {
		return &ListLabelsOutput{Body: labels}, nil
	}
}
