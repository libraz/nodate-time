package invites

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	WorkspaceID uint32
}

// defaultMemberColor is the colour a member joining through a link starts
// with; they can change it afterwards.
const defaultMemberColor = "#42A5F5"

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

func toAPIError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
}

func nullStringValue(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

// hashToken is what the database stores. The plaintext exists only in the
// link shown once at creation, so reading every row of this table yields no
// working invite.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
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
func generateToken() (string, error) {
	out := make([]byte, tokenLength)
	buf := make([]byte, tokenLength*2)
	bi := len(buf)
	for i := 0; i < tokenLength; {
		if bi >= len(buf) {
			if _, err := rand.Read(buf); err != nil {
				return "", err
			}
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
	return string(out), nil
}

// resolveCalendarManage resolves a calendar and admits only the roles that
// may hand out access to it.
func resolveCalendarManage(ctx context.Context, deps Deps, calPubID string) (generated.Calendar, error) {
	userID, _ := middleware.ActorFromContext(ctx)
	return calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// mapInvite renders a stored invite. It cannot include the token: only the
// hash is stored, and that is the point.
func mapInvite(inv generated.CalendarInvite) InviteResponse {
	resp := InviteResponse{
		ID:        pubIDToHex(inv.PublicID),
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
		cal, err := resolveCalendarManage(ctx, deps, in.CalendarID)
		if err != nil {
			return nil, toAPIError(err)
		}

		userID, _ := middleware.ActorFromContext(ctx)
		token, err := generateToken()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		role := in.Body.Role
		if role == "" {
			role = string(generated.CalendarInvitesRoleEditor)
		}
		// A link may not grant a role that can hand out further access;
		// promotion to manager or owner goes through UpdateMemberRole, which
		// requires somebody who already holds it.
		switch generated.CalendarInvitesRole(role) {
		case generated.CalendarInvitesRoleEditor, generated.CalendarInvitesRoleViewer:
		default:
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		isPublic := in.Body.IsPublic != nil && *in.Body.IsPublic
		// A public link publishes the calendar read-only: it never grants
		// membership, so the role it nominally carries is forced to the
		// least privilege and its use is not counted against a limit.
		if isPublic {
			role = string(generated.CalendarInvitesRoleViewer)
		}

		var maxUses sql.NullInt32
		if !isPublic && in.Body.MaxUses != nil {
			maxUses = sql.NullInt32{Int32: *in.Body.MaxUses, Valid: true}
		}

		var expiresAt sql.NullTime
		if in.Body.ExpiresInHours != nil {
			expiresAt = sql.NullTime{Time: time.Now().Add(time.Duration(*in.Body.ExpiresInHours) * time.Hour), Valid: true}
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var created generated.CalendarInvite
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if isPublic {
				// Lock the calendar row so two concurrent requests cannot both
				// find no public link and both create one.
				if _, err := q.GetCalendarByIDForUpdate(ctx, cal.ID); err != nil {
					return err
				}
				count, err := q.CountActivePublicInvites(ctx, cal.ID)
				if err != nil {
					return err
				}
				if count > 0 {
					return apierrors.InvitePublicAlreadyExists
				}
			}
			if _, err := q.CreateInvite(ctx, generated.CreateInviteParams{
				PublicID:        pubID[:],
				WorkspaceID:     deps.WorkspaceID,
				CalendarID:      cal.ID,
				CreatedByUserID: userID,
				TokenHash:       hashToken(token),
				Role:            generated.CalendarInvitesRole(role),
				MaxUses:         maxUses,
				ExpiresAt:       expiresAt,
				IsPublic:        isPublic,
			}); err != nil {
				return err
			}
			var err error
			created, err = q.GetInviteByTokenHash(ctx, hashToken(token))
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeInviteCreated,
				Summary:     role,
				Subject:     pubID[:],
				Extra:       map[string]any{"public": isPublic},
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		out := &CreateInviteOutput{}
		out.Body = mapInvite(created)
		// The only moment the plaintext is ever returned.
		out.Body.Token = token
		return out, nil
	}
}

func AcceptInvite(deps Deps) func(context.Context, *AcceptInviteInput) (*AcceptInviteOutput, error) {
	return func(ctx context.Context, in *AcceptInviteInput) (*AcceptInviteOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)

		invite, err := deps.Queries.GetLiveInviteByTokenHash(ctx, hashToken(in.Token))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InviteNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// A link that has run out is a different answer from one that never
		// existed, and the holder is entitled to it: they have the token, so
		// nothing is disclosed, and "expired" is what prompts asking for
		// another rather than concluding the calendar is gone.
		if invite.ExpiresAt.Valid && !invite.ExpiresAt.Time.After(time.Now()) {
			return nil, apierrors.ToHuma(apierrors.InviteExpired)
		}
		if invite.MaxUses.Valid && invite.UseCount >= uint32(invite.MaxUses.Int32) {
			return nil, apierrors.ToHuma(apierrors.InviteExpired)
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

		// A public link publishes the calendar for reading; it must never
		// grant membership.
		if invite.IsPublic {
			return nil, apierrors.ToHuma(apierrors.InvitePublicViewOnly)
		}

		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		memberPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		alreadyMember := false
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Claim a use atomically; zero rows means the link is exhausted.
			// Checking the limit inside the UPDATE is what makes two
			// concurrent acceptances of a single-use link race safely.
			res, err := q.ConsumeInviteUse(ctx, invite.ID)
			if err != nil {
				return err
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return apierrors.InviteExpired
			}

			if _, err := q.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
				PublicID:        memberPubID[:],
				WorkspaceID:     deps.WorkspaceID,
				CalendarID:      invite.CalendarID,
				UserID:          userID,
				Role:            generated.CalendarMembersRole(invite.Role),
				MemberColor:     defaultMemberColor,
				InvitedByUserID: sql.NullInt32{Int32: int32(invite.CreatedByUserID), Valid: true},
			}); err != nil {
				if isDuplicateKey(err) {
					alreadyMember = true
					return nil
				}
				return err
			}

			summary := string(invite.Role)
			if user.DisplayName != "" {
				summary = user.DisplayName + " -> " + string(invite.Role)
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  invite.CalendarID,
				ActorUserID: userID,
				Type:        eventlog.TypeMemberJoined,
				Summary:     summary,
				Subject:     user.PublicID,
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		if alreadyMember {
			if current, cerr := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
				CalendarID: invite.CalendarID,
				UserID:     userID,
			}); cerr == nil {
				out.Body.Role = string(current.Role)
				return out, nil
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out.Body.Role = string(invite.Role)
		return out, nil
	}
}

func ListInvites(deps Deps) func(context.Context, *ListInvitesInput) (*ListInvitesOutput, error) {
	return func(ctx context.Context, in *ListInvitesInput) (*ListInvitesOutput, error) {
		cal, err := resolveCalendarManage(ctx, deps, in.CalendarID)
		if err != nil {
			return nil, toAPIError(err)
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
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarManage(ctx, deps, in.CalendarID)
		if err != nil {
			return nil, toAPIError(err)
		}

		invitePub, err := parseUUID(in.InviteID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InviteNotFound)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			res, err := q.RevokeInviteByPublicIDAndCalendar(ctx, generated.RevokeInviteByPublicIDAndCalendarParams{
				PublicID:   invitePub,
				CalendarID: cal.ID,
			})
			if err != nil {
				return err
			}
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				// The id does not exist, or belongs to a calendar the caller
				// did not resolve. Either way nothing was revoked, so nothing
				// is logged: an entry here would record a change that never
				// happened.
				return apierrors.InviteNotFound
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeInviteRevoked,
				Subject:     invitePub,
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		return &DeleteInviteOutput{}, nil
	}
}

// --- Public share handlers (no auth required) ---

func PublicCalendar(deps Deps) func(context.Context, *PublicCalendarInput) (*PublicCalendarOutput, error) {
	return func(ctx context.Context, in *PublicCalendarInput) (*PublicCalendarOutput, error) {
		row, err := deps.Queries.GetInviteByTokenHashWithCalendar(ctx, hashToken(in.Token))
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
		out.Body.Spent = inviteSpent(row.MaxUses, row.UseCount)
		out.Body.Joinable = !row.IsPublic && !out.Body.Spent
		return out, nil
	}
}

// inviteSpent reports whether a link has been followed as many times as it was
// meant to be. The page that opens a link is served before anyone is asked to
// sign in, so this is the only place the difference can be shown: without it
// the visitor is offered a join that the accept endpoint then refuses, and
// nothing on the way there said the link was finished.
func inviteSpent(maxUses sql.NullInt32, useCount uint32) bool {
	if !maxUses.Valid {
		return false
	}
	return int64(useCount) >= int64(maxUses.Int32)
}

// publicEventFields decides what a link holder may see of an event. A
// calendar published read-only still contains events whose visibility says
// their details are not for everyone, and the link does not override that:
// those show as taken time with no title.
func publicEventFields(e generated.CalendarEvent) (title, location string, private bool) {
	if e.Visibility == generated.CalendarEventsVisibilityPrivate {
		return "", "", true
	}
	return e.Title, nullStringValue(e.Location), false
}

// publishedToLink reports whether an event belongs in a public feed at all.
//
// Private and confidential differ in kind, not degree: private hides the
// details but still says the time is taken, while confidential says the event
// is nobody else's business. Publishing a confidential event as an anonymous
// busy block still tells a stranger when its owner is occupied, which is the
// thing the setting exists to withhold.
func publishedToLink(e generated.CalendarEvent) bool {
	return e.Visibility != generated.CalendarEventsVisibilityConfidential
}

func PublicEvents(deps Deps) func(context.Context, *PublicEventsInput) (*PublicEventsOutput, error) {
	return func(ctx context.Context, in *PublicEventsInput) (*PublicEventsOutput, error) {
		// Only a link that publishes the calendar may serve its events. A
		// join link is an offer of access, not access: it grants membership
		// on acceptance and nothing before it.
		row, err := deps.Queries.GetPublicInviteByTokenHash(ctx, hashToken(in.Token))
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InviteNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		calendarID := row.CalendarID
		calendarColor := row.CalendarColor

		loc := daterange.Location(in.TZ, "")
		window := daterange.Default(in.Days, loc)
		if in.StartDate != "" && in.EndDate != "" {
			window, err = daterange.Parse(in.StartDate, in.EndDate, loc)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
		}
		startTime, endTime := window.Start, window.End

		render := func(e generated.CalendarEvent, id string, startAt, endAt time.Time) PublicEventResponse {
			title, location, private := publicEventFields(e)
			return PublicEventResponse{
				ID:       id,
				Title:    title,
				AllDay:   e.AllDay,
				StartAt:  startAt,
				EndAt:    endAt,
				Timezone: e.Timezone,
				Color:    calendarColor,
				Location: location,
				Private:  private,
			}
		}

		rows, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: calendarID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var results []PublicEventResponse
		for _, e := range rows {
			if !e.StartAt.Valid || !e.EndAt.Valid || !publishedToLink(e) {
				continue
			}
			results = append(results, render(e, pubIDToHex(e.PublicID), e.StartAt.Time, e.EndAt.Time))
		}

		recurringRows, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: calendarID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		seriesIDs := make([]uint32, 0, len(recurringRows))
		for _, e := range recurringRows {
			seriesIDs = append(seriesIDs, e.ID)
		}
		overrides := eventexpand.LoadOverrides(ctx, deps.Queries, seriesIDs)

		truncated := false
		for _, e := range recurringRows {
			if !publishedToLink(e) {
				continue
			}
			if len(results) >= daterange.MaxInstances {
				truncated = true
				break
			}
			parentID := pubIDToHex(e.PublicID)
			for _, inst := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], startTime, endTime) {
				// This endpoint takes no token beyond the link itself, so the
				// cost of one request is whatever the link's holder asks for.
				if len(results) >= daterange.MaxInstances {
					truncated = true
					break
				}
				// The series' own zone decides the day, matching the ids the
				// authenticated event API hands out for the same occurrences.
				dateStr := inst.OriginalStart.In(recurrence.LoadLocation(e.Timezone)).Format("20060102")
				id := fmt.Sprintf("%s_%s", parentID, dateStr)
				if inst.IsOverride {
					if !inst.Event.StartAt.Valid || !inst.Event.EndAt.Valid || !publishedToLink(inst.Event) {
						continue
					}
					results = append(results, render(inst.Event, id, inst.Event.StartAt.Time, inst.Event.EndAt.Time))
					continue
				}
				results = append(results, render(e, id, inst.Occurrence.StartAt, inst.Occurrence.EndAt))
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].StartAt.Before(results[j].StartAt)
		})

		out := &PublicEventsOutput{Body: results}
		if truncated {
			out.Truncated = "true"
		}
		if out.Body == nil {
			out.Body = []PublicEventResponse{}
		}
		return out, nil
	}
}
