package invites

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

type Deps struct {
	DB      *sql.DB
	Queries *generated.Queries
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// resolveCalendarAdmin resolves a calendar by public ID and verifies admin role.
func resolveCalendarAdmin(ctx context.Context, deps Deps, calPubID string) (generated.Calendar, error) {
	userID, _ := middleware.ActorFromContext(ctx)
	pubBytes, err := parseUUID(calPubID)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	member, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID, UserID: userID,
	})
	if err != nil || member.Role != "admin" {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

func mapInvite(inv generated.CalendarInvite) InviteResponse {
	resp := InviteResponse{
		ID:       inv.ID,
		Token:    inv.Token,
		Role:     string(inv.Role),
		UseCount: inv.UseCount,
		CreatedAt: inv.CreatedAt,
	}
	if inv.MaxUses.Valid {
		v := uint32(inv.MaxUses.Int32)
		resp.MaxUses = &v
	}
	if inv.ExpiresAt.Valid {
		resp.ExpiresAt = &inv.ExpiresAt.Time
	}
	return resp
}

func CreateInvite(deps Deps) func(context.Context, *CreateInviteInput) (*CreateInviteOutput, error) {
	return func(ctx context.Context, in *CreateInviteInput) (*CreateInviteOutput, error) {
		cal, err := resolveCalendarAdmin(ctx, deps, in.CalendarID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		userID, _ := middleware.ActorFromContext(ctx)
		token := generateToken()
		role := in.Body.Role
		if role == "" {
			role = "member"
		}

		var maxUses sql.NullInt32
		if in.Body.MaxUses != nil {
			maxUses = sql.NullInt32{Int32: *in.Body.MaxUses, Valid: true}
		}

		result, err := deps.Queries.CreateInvite(ctx, generated.CreateInviteParams{
			CalendarID: cal.ID,
			Token:      token,
			Role:       generated.CalendarInvitesRole(role),
			MaxUses:    maxUses,
			ExpiresAt:  sql.NullTime{},
			CreatedBy:  userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		inviteID, _ := result.LastInsertId()
		out := &CreateInviteOutput{}
		out.Body = InviteResponse{ID: uint32(inviteID), Token: token, Role: role, UseCount: 0, CreatedAt: time.Now()}
		return out, nil
	}
}

func AcceptInvite(deps Deps) func(context.Context, *AcceptInviteInput) (*AcceptInviteOutput, error) {
	return func(ctx context.Context, in *AcceptInviteInput) (*AcceptInviteOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)

		invite, err := deps.Queries.GetInviteByToken(ctx, in.Token)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InviteNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		qtx := deps.Queries.WithTx(tx)

		// Add member
		_, err = qtx.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
			CalendarID: invite.CalendarID,
			UserID:     userID,
			Role:       generated.CalendarMembersRole(invite.Role),
			Color:      "#42A5F5",
		})
		if err != nil {
			// Might already be a member
			return nil, apierrors.ToHuma(apierrors.MemberAlreadyExists)
		}

		err = qtx.IncrementInviteUseCount(ctx, invite.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		cal, _ := deps.Queries.GetCalendarByPublicID(ctx, nil)
		// Get calendar public ID
		calRow, _ := deps.Queries.ListCalendarsByUser(ctx, userID)
		calPubID := ""
		for _, c := range calRow {
			if c.ID == invite.CalendarID {
				calPubID = pubIDToHex(c.PublicID)
				break
			}
		}

		out := &AcceptInviteOutput{}
		out.Body.CalendarID = calPubID
		out.Body.Role = string(invite.Role)
		_ = cal
		return out, nil
	}
}

func ListInvites(deps Deps) func(context.Context, *ListInvitesInput) (*ListInvitesOutput, error) {
	return func(ctx context.Context, in *ListInvitesInput) (*ListInvitesOutput, error) {
		cal, err := resolveCalendarAdmin(ctx, deps, in.CalendarID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		rows, err := deps.Queries.ListInvitesByCalendar(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListInvitesOutput{Body: make([]InviteResponse, 0, len(rows))}
		for _, inv := range rows {
			out.Body = append(out.Body, mapInvite(inv))
		}
		return out, nil
	}
}

func DeleteInviteHandler(deps Deps) func(context.Context, *DeleteInviteInput) (*DeleteInviteOutput, error) {
	return func(ctx context.Context, in *DeleteInviteInput) (*DeleteInviteOutput, error) {
		cal, err := resolveCalendarAdmin(ctx, deps, in.CalendarID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		err = deps.Queries.DeleteInviteByIDAndCalendar(ctx, generated.DeleteInviteByIDAndCalendarParams{
			ID:         in.InviteID,
			CalendarID: cal.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteInviteOutput{}, nil
	}
}

// --- Public share handlers (no auth required) ---

func PublicCalendar(deps Deps) func(context.Context, *PublicCalendarInput) (*PublicCalendarOutput, error) {
	return func(ctx context.Context, in *PublicCalendarInput) (*PublicCalendarOutput, error) {
		row, err := deps.Queries.GetInviteByTokenPublic(ctx, in.Token)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InviteNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &PublicCalendarOutput{}
		out.Body.CalendarID = pubIDToHex(row.CalendarPublicID)
		out.Body.Name = row.CalendarName
		out.Body.Color = row.CalendarColor
		return out, nil
	}
}

func PublicEvents(deps Deps) func(context.Context, *PublicEventsInput) (*PublicEventsOutput, error) {
	return func(ctx context.Context, in *PublicEventsInput) (*PublicEventsOutput, error) {
		// Validate token exists
		_, err := deps.Queries.GetInviteByTokenPublic(ctx, in.Token)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InviteNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var startTime, endTime time.Time
		if in.StartDate != "" && in.EndDate != "" {
			startTime, _ = time.Parse("2006-01-02", in.StartDate)
			endTime, _ = time.Parse("2006-01-02", in.EndDate)
			endTime = endTime.AddDate(0, 0, 1)
		} else {
			startTime = time.Now().AddDate(0, 0, -7)
			endTime = time.Now().AddDate(0, 0, in.Days)
		}

		// Fetch non-recurring events
		rows, err := deps.Queries.ListEventsByInviteCalendar(ctx, generated.ListEventsByInviteCalendarParams{
			Token:   in.Token,
			StartAt: endTime,
			EndAt:   startTime,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var results []PublicEventResponse
		for _, e := range rows {
			results = append(results, PublicEventResponse{
				ID:       pubIDToHex(e.PublicID),
				Title:    e.Title,
				AllDay:   e.AllDay,
				StartAt:  e.StartAt,
				EndAt:    e.EndAt,
				Color:    e.Color,
				Location: e.Location,
			})
		}

		// Fetch and expand recurring events
		recurringRows, err := deps.Queries.ListRecurringEventsByInviteCalendar(ctx, generated.ListRecurringEventsByInviteCalendarParams{
			Token:         in.Token,
			StartAt:       endTime,
			RecurrenceEnd: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		for _, e := range recurringRows {
			var ruleData json.RawMessage
			if e.RecurrenceRule != nil {
				ruleData = *e.RecurrenceRule
			}
			rule := recurrence.ParseRule(ruleData)
			if rule == nil {
				continue
			}
			occurrences := recurrence.Expand(rule, e.StartAt, e.EndAt, startTime, endTime)
			parentID := pubIDToHex(e.PublicID)
			for _, occ := range occurrences {
				dateStr := occ.StartAt.Format("20060102")
				results = append(results, PublicEventResponse{
					ID:       fmt.Sprintf("%s_%s", parentID, dateStr),
					Title:    e.Title,
					AllDay:   e.AllDay,
					StartAt:  occ.StartAt,
					EndAt:    occ.EndAt,
					Color:    e.Color,
					Location: e.Location,
				})
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].StartAt.Before(results[j].StartAt)
		})

		out := &PublicEventsOutput{Body: results}
		if out.Body == nil {
			out.Body = []PublicEventResponse{}
		}
		return out, nil
	}
}
