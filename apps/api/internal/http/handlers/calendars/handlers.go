package calendars

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
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
	cal, _, err := resolveCalendarMember(ctx, deps, calendarPubID, userID)
	return cal, err
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := resolveCalendarMember(ctx, deps, calendarPubID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if member.Role == generated.CalendarMembersRoleViewer {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

// resolveCalendarMember resolves the calendar row and the requesting user's
// membership, returning both for callers that need the member's role.
func resolveCalendarMember(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	pubBytes, err := parseUUID(calendarPubID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.InternalUnexpected
	}
	member, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	})
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarAccessDenied
	}
	return cal, member, nil
}

func ListCalendars(deps Deps) func(context.Context, *ListCalendarsInput) (*ListCalendarsOutput, error) {
	return func(ctx context.Context, _ *ListCalendarsInput) (*ListCalendarsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		rows, err := deps.Queries.ListCalendarsByUser(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// Mark calendars that currently expose an active public link.
		publicSet := map[uint32]bool{}
		if ids, err := deps.Queries.ListPublicSharedCalendarIDs(ctx, userID); err == nil {
			for _, id := range ids {
				publicSet[id] = true
			}
		}

		out := &ListCalendarsOutput{Body: make([]CalendarResponse, 0, len(rows))}
		for _, c := range rows {
			resp := mapCalendar(c)
			resp.PublicShared = publicSet[c.ID]
			out.Body = append(out.Body, resp)
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
		resp := mapCalendar(cal)
		if cnt, err := deps.Queries.CountActivePublicInvites(ctx, cal.ID); err == nil {
			resp.PublicShared = cnt > 0
		}
		return &GetCalendarOutput{Body: resp}, nil
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
		// Check admin role
		member, _ := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
			CalendarID: cal.ID,
			UserID:     userID,
		})
		if member.Role != "admin" {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		// Color and cover are optional: when omitted, keep the current values so a
		// rename does not blank them out.
		color := cal.Color
		if in.Body.Color != nil {
			color = *in.Body.Color
		}
		coverURL := cal.CoverUrl
		if in.Body.CoverURL != nil {
			coverURL = *in.Body.CoverURL
		}
		err = deps.Queries.UpdateCalendar(ctx, generated.UpdateCalendarParams{
			Name:     in.Body.Name,
			Color:    color,
			CoverUrl: coverURL,
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
				ID:    pubIDToHex(m.UserPublicID),
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

func UpdateMemberRole(deps Deps) func(context.Context, *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
	return func(ctx context.Context, in *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		actor, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: actorID})
		if err != nil || actor.Role != "admin" {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		// Admins manage other members' roles, not their own. Changing your own role
		// must go through another admin.
		if target.ID == actorID {
			return nil, apierrors.ToHuma(apierrors.MemberSelfModify)
		}
		current, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: target.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		// Lock the admin count and apply the role change atomically so concurrent
		// demotions cannot drop the calendar below one admin.
		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		qtx := deps.Queries.WithTx(tx)

		if current.Role == "admin" && in.Body.Role != "admin" {
			adminCount, err := qtx.CountCalendarAdminsForUpdate(ctx, cal.ID)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if adminCount <= 1 {
				return nil, apierrors.ToHuma(apierrors.MemberLastAdmin)
			}
		}

		if err := qtx.UpdateCalendarMemberRole(ctx, generated.UpdateCalendarMemberRoleParams{
			Role:       generated.CalendarMembersRole(in.Body.Role),
			CalendarID: cal.ID,
			UserID:     target.ID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &UpdateMemberRoleOutput{Body: MemberResponse{
			ID:    pubIDToHex(target.PublicID),
			Name:  target.Name,
			Email: target.Email,
			Icon:  target.Icon,
			Role:  in.Body.Role,
			Color: current.Color,
		}}, nil
	}
}

func RemoveMember(deps Deps) func(context.Context, *RemoveMemberInput) (*RemoveMemberOutput, error) {
	return func(ctx context.Context, in *RemoveMemberInput) (*RemoveMemberOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		current, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: target.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		// Only admins remove members, and they cannot remove themselves.
		if target.ID == actorID {
			return nil, apierrors.ToHuma(apierrors.MemberSelfModify)
		}
		actor, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: actorID})
		if err != nil || actor.Role != "admin" {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		// Lock the admin count and remove the member atomically so concurrent
		// removals cannot drop the calendar below one admin.
		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		qtx := deps.Queries.WithTx(tx)

		if current.Role == "admin" {
			adminCount, err := qtx.CountCalendarAdminsForUpdate(ctx, cal.ID)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if adminCount <= 1 {
				return nil, apierrors.ToHuma(apierrors.MemberLastAdmin)
			}
		}

		if err := qtx.RemoveCalendarMember(ctx, generated.RemoveCalendarMemberParams{
			CalendarID: cal.ID,
			UserID:     target.ID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &RemoveMemberOutput{}, nil
	}
}

// ListLabels returns the predefined color palette. Names are returned as i18n
// keys (label.1 .. label.10) so the frontend can localize them.
func ListLabels(_ Deps) func(context.Context, *ListLabelsInput) (*ListLabelsOutput, error) {
	colors := []string{
		"#47B2F7", "#F35F8C", "#B38BDC", "#FDC02D", "#E73B3B",
		"#2ECC87", "#F5A623", "#8F8F8F", "#42A5F5", "#FF7043",
	}
	labels := make([]LabelResponse, len(colors))
	for i, c := range colors {
		id := strconv.Itoa(i + 1)
		labels[i] = LabelResponse{ID: id, NameKey: "label." + id, Color: c}
	}
	return func(_ context.Context, _ *ListLabelsInput) (*ListLabelsOutput, error) {
		return &ListLabelsOutput{Body: labels}, nil
	}
}
