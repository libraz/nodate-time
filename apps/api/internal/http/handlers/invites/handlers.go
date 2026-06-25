package invites

import (
	"context"
	"crypto/rand"
	"database/sql"
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

// tokenAlphabet is base62 (0-9, A-Z, a-z): URL-safe, fully alphanumeric, and
// dense enough to keep share links short while avoiding padding or symbols.
const tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// tokenLength yields ~131 bits of entropy (22 * log2(62)), comfortably above
// the 128-bit bar for an unguessable capability token.
const tokenLength = 22

// generateToken returns a random base62 invite token. crypto/rand drives the
// choice, and rejecting byte values >= 248 (the largest multiple of 62 below
// 256) removes the modulo bias a plain b%62 would introduce.
func generateToken() string {
	out := make([]byte, tokenLength)
	buf := make([]byte, tokenLength*2)
	bi := len(buf)
	for i := 0; i < tokenLength; {
		if bi >= len(buf) {
			rand.Read(buf)
			bi = 0
		}
		b := buf[bi]
		bi++
		if b >= 248 {
			continue
		}
		out[i] = tokenAlphabet[b%62]
		i++
	}
	return string(out)
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
		ID:        inv.ID,
		Token:     inv.Token,
		Role:      string(inv.Role),
		UseCount:  inv.UseCount,
		IsPublic:  inv.IsPublic,
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
		// Invite links may not grant admin; admin promotion goes through UpdateMemberRole.
		if role == "admin" {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		isPublic := in.Body.IsPublic != nil && *in.Body.IsPublic
		// A public link is a read-only embed link: force viewer role and unlimited
		// uses so it never grants membership and never expires through use.
		if isPublic {
			role = "viewer"
		}

		var maxUses sql.NullInt32
		if !isPublic && in.Body.MaxUses != nil {
			maxUses = sql.NullInt32{Int32: *in.Body.MaxUses, Valid: true}
		}

		var expiresAt sql.NullTime
		if in.Body.ExpiresInHours != nil {
			expiresAt = sql.NullTime{Time: time.Now().Add(time.Duration(*in.Body.ExpiresInHours) * time.Hour), Valid: true}
		}

		result, err := deps.Queries.CreateInvite(ctx, generated.CreateInviteParams{
			CalendarID: cal.ID,
			Token:      token,
			Role:       generated.CalendarInvitesRole(role),
			MaxUses:    maxUses,
			ExpiresAt:  expiresAt,
			CreatedBy:  userID,
			IsPublic:   isPublic,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		inviteID, _ := result.LastInsertId()
		out := &CreateInviteOutput{}
		out.Body = InviteResponse{
			ID:        uint32(inviteID),
			Token:     token,
			Role:      role,
			UseCount:  0,
			IsPublic:  isPublic,
			CreatedAt: time.Now(),
		}
		if maxUses.Valid {
			v := uint32(maxUses.Int32)
			out.Body.MaxUses = &v
		}
		if expiresAt.Valid {
			out.Body.ExpiresAt = &expiresAt.Time
		}
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

		cal, err := deps.Queries.GetCalendarByID(ctx, invite.CalendarID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &AcceptInviteOutput{}
		out.Body.CalendarID = pubIDToHex(cal.PublicID)

		// Idempotent re-accept: an existing member must not burn a use.
		existing, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
			CalendarID: invite.CalendarID,
			UserID:     userID,
		})
		if err == nil {
			out.Body.Role = string(existing.Role)
			return out, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// A public link is for read-only embedding only; it must never grant
		// membership. Block non-members from joining through it.
		if invite.IsPublic {
			return nil, apierrors.ToHuma(apierrors.InvitePublicViewOnly)
		}

		tx, err := deps.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		defer tx.Rollback()
		qtx := deps.Queries.WithTx(tx)

		// Atomically claim a use; 0 rows means the invite is exhausted (race-safe).
		res, err := qtx.ConsumeInviteUse(ctx, invite.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if affected, _ := res.RowsAffected(); affected != 1 {
			return nil, apierrors.ToHuma(apierrors.InviteExpired)
		}

		_, err = qtx.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
			CalendarID: invite.CalendarID,
			UserID:     userID,
			Role:       generated.CalendarMembersRole(invite.Role),
			Color:      "#42A5F5",
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := tx.Commit(); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out.Body.Role = string(invite.Role)
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
		out.Body.Joinable = !row.IsPublic
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
