package calendars

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	Storage     *storage.Client
	WorkspaceID uint32
	// AppName names the software in the PRODID of an exported calendar, which
	// is what the importing client reports as the file's source.
	AppName string
}

// maxCandidatesListed is one screen of a picker; a larger workspace is
// narrowed by searching rather than scrolled.
const maxCandidatesListed = 50

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	b := u[:]
	return b, nil
}

func nullStringValue(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// mapCalendar renders a calendar together with the requesting user's own
// standing on it. The role and the member colour come from the membership
// that was resolved to reach the calendar in the first place, so every
// response carries the answer the client would otherwise have to reconstruct.
func mapCalendar(c generated.Calendar, role generated.CalendarMembersRole, memberColor string) CalendarResponse {
	return CalendarResponse{
		ID:          pubIDToHex(c.PublicID),
		ETag:        calendarETag(c),
		Name:        c.Name,
		Color:       c.Color,
		CreatedAt:   c.CreatedAt,
		Role:        string(role),
		MemberColor: memberColor,
	}
}

// calendarETag is the strong tag of a calendar's current revision, the time of
// its last write at the millisecond the column holds.
func calendarETag(c generated.Calendar) string {
	stamp := c.CreatedAt
	if c.UpdatedAt.Valid {
		stamp = c.UpdatedAt.Time
	}
	return `"` + stamp.UTC().Format("20060102T150405.000") + `"`
}

// checkIfMatch compares the caller's If-Match with the calendar's current tag.
func checkIfMatch(header precondition.IfMatch, c generated.Calendar) error {
	err := header.Check(calendarETag(c))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, precondition.ErrStale):
		return apierrors.CalendarStale
	case errors.Is(err, precondition.ErrMalformed):
		return apierrors.BadIfMatch
	}
	return err
}

// publicShared reports whether the calendar currently exposes an active
// public link. A response that leaves it out reads as "not shared", so every
// calendar body answers the question rather than defaulting it.
func publicShared(ctx context.Context, deps Deps, calendarID uint32) bool {
	cnt, err := deps.Queries.CountActivePublicInvites(ctx, calendarID)
	return err == nil && cnt > 0
}

// withShareState fills in who the calendar is shared with. A response that
// left these at their zero values would read as a calendar nobody shares.
func withShareState(ctx context.Context, deps Deps, resp CalendarResponse, calendarID uint32) (CalendarResponse, error) {
	count, err := deps.Queries.CountCalendarMembers(ctx, calendarID)
	if err != nil {
		return resp, err
	}
	resp.MemberCount = int(count)
	resp.PublicShared = publicShared(ctx, deps, calendarID)
	return resp, nil
}

// resolveCalendar converts public UUID to internal calendar row + verifies membership.
func resolveCalendar(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
}

// resolveCalendarManage resolves the calendar and admits only roles that may
// change its settings or membership. It replaces the older pattern of
// resolving for read and then checking the role separately -- two steps that
// could drift apart, which is exactly how an authorization check goes
// missing.
//
// It returns the caller's membership alongside the calendar, so a handler
// that reports the caller's own role back does not have to look it up again.
func resolveCalendarManage(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	return calresolve.ManageMember(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
}

// resolveCalendarOwn resolves the calendar and admits only its owner, for the
// operations that are not merely administrative -- handing ownership out or
// taking it away, and destroying the calendar for everyone on it.
func resolveCalendarOwn(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Own(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Write(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
}

// resolveCalendarMember resolves the calendar row and the requesting user's
// membership, returning both for callers that need the member's role.
func resolveCalendarMember(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	return calresolve.Member(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
}

func ListCalendars(deps Deps) func(context.Context, *ListCalendarsInput) (*ListCalendarsOutput, error) {
	return func(ctx context.Context, _ *ListCalendarsInput) (*ListCalendarsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		rows, err := deps.Queries.ListCalendarsByUser(ctx, generated.ListCalendarsByUserParams{
			UserID:      userID,
			WorkspaceID: deps.WorkspaceID,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		// Mark calendars that currently expose an active public link.
		publicSet := map[uint32]bool{}
		if ids, err := deps.Queries.ListPublicSharedCalendarIDs(ctx, userID); err == nil {
			for _, id := range ids {
				publicSet[id] = true
			}
		}

		out := &ListCalendarsOutput{Body: make([]CalendarResponse, 0, len(rows))}
		coverKeys := map[uint32]string{}
		if len(rows) > 0 {
			calendarIDs := make([]uint32, 0, len(rows))
			for _, c := range rows {
				calendarIDs = append(calendarIDs, c.ID)
			}
			coverRows, err := deps.Queries.ListCalendarCoverKeys(ctx, calendarIDs)
			if err != nil {
				return nil, apierrors.Internal(ctx, err)
			}
			coverKeys = make(map[uint32]string, len(coverRows))
			for _, cover := range coverRows {
				coverKeys[cover.CalendarID] = cover.StorageKey
			}
		}
		for _, c := range rows {
			calendar := generated.Calendar{
				ID:          c.ID,
				PublicID:    c.PublicID,
				Kind:        c.Kind,
				Name:        c.Name,
				Color:       c.Color,
				OwnerUserID: c.OwnerUserID,
				UpdatedAt:   c.UpdatedAt,
				CreatedAt:   c.CreatedAt,
			}
			response := mapCalendarWithCover(deps, calendar, c.MemberRole, c.MemberColor, coverKeys[c.ID])
			response.PublicShared = publicSet[c.ID]
			response.MemberCount = int(c.MemberCount)
			out.Body = append(out.Body, response)
		}
		return out, nil
	}
}

func GetCalendar(deps Deps) func(context.Context, *GetCalendarInput) (*GetCalendarOutput, error) {
	return func(ctx context.Context, in *GetCalendarInput) (*GetCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, member, err := resolveCalendarMember(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		coverKey, err := calendarCoverKey(ctx, deps.Queries, cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		resp, err := withShareState(ctx, deps, mapCalendarWithCover(deps, cal, member.Role, member.MemberColor, coverKey), cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &GetCalendarOutput{ETag: resp.ETag, Body: resp}, nil
	}
}

func CreateCalendar(deps Deps) func(context.Context, *CreateCalendarInput) (*CreateCalendarOutput, error) {
	return func(ctx context.Context, in *CreateCalendarInput) (*CreateCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)

		var created generated.Calendar
		err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.RequireRoomForCalendar(ctx, q, deps.WorkspaceID, userID); err != nil {
				return err
			}
			var err error
			created, err = calresolve.SetUp(ctx, q, deps.WorkspaceID, userID, in.Body.Name, in.Body.Color)
			return err
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		// The creator owns the calendar, in the colour it was just created
		// with. Nothing has had the chance to share it yet, so the creator is
		// its only member and publicShared is false by construction.
		resp := mapCalendarWithCover(deps, created, generated.CalendarMembersRoleOwner, created.Color, "")
		resp.MemberCount = 1
		return &CreateCalendarOutput{Body: resp}, nil
	}
}

func UpdateCalendar(deps Deps) func(context.Context, *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
	return func(ctx context.Context, in *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		if in.Body.Name.IsNull() || in.Body.Color.IsNull() {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		cal, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		var updated generated.Calendar
		var member generated.CalendarMember
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, cal.ID); err != nil {
				return err
			}
			// The caller's role and the row being merged over are both read
			// here, under the lock, so a manager removed or a field changed
			// since the first read is seen rather than written back over.
			var err error
			if member, err = calresolve.LockManage(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			current, err := q.GetCalendarByIDForUpdate(ctx, cal.ID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, current); err != nil {
				return err
			}

			// Absent members keep the current values so a partial update does
			// not blank out other fields; neither member can be cleared.
			name := current.Name
			if v, ok := in.Body.Name.Get(); ok {
				name = v
			}
			color := current.Color
			if v, ok := in.Body.Color.Get(); ok {
				color = v
			}
			if err := q.UpdateCalendar(ctx, generated.UpdateCalendarParams{
				Name:  name,
				Color: color,
				ID:    cal.ID,
			}); err != nil {
				return err
			}
			updated, err = q.GetCalendarByID(ctx, cal.ID)
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeCalendarEdited,
				Summary:     name,
				Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		// A rename says nothing about who the calendar is shared with, so the
		// share state is read rather than left at its zero value: a client
		// that merges this response over the one it holds would otherwise take
		// the omission for a calendar that had stopped being shared.
		coverKey, err := calendarCoverKey(ctx, deps.Queries, cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		resp, err := withShareState(ctx, deps, mapCalendarWithCover(deps, updated, member.Role, member.MemberColor, coverKey), cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &UpdateCalendarOutput{ETag: resp.ETag, Body: resp}, nil
	}
}

func DeleteCalendar(deps Deps) func(context.Context, *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
	return func(ctx context.Context, in *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarOwn(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockExclusive(ctx, q, cal.ID); err != nil {
				return err
			}
			// Ownership is re-read under the lock: an owner demoted since the
			// first read no longer destroys the calendar for everyone.
			owner, err := calresolve.LockMember(ctx, q, cal.ID, userID)
			if err != nil {
				return err
			}
			if !calresolve.CanOwn(owner.Role) {
				return apierrors.CalendarRoleRequired
			}
			current, err := q.GetCalendarByIDForUpdate(ctx, cal.ID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, current); err != nil {
				return err
			}
			// Retire this calendar's attachments. A retired row keeps its
			// reference on the blob until the cleanup sweep removes it, so the
			// counts do not change here.
			if err := q.SoftDeleteAttachmentsByCalendar(ctx, cal.ID); err != nil {
				return err
			}
			// The calendar_album_photo_disable trigger retires its album photos.
			if err := q.SoftDeleteCalendar(ctx, cal.ID); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeCalendarGone,
				Summary:     cal.Name,
				Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &DeleteCalendarOutput{}, nil
	}
}

// memberEmail returns the address the caller may read for a member: their own,
// or anyone's when the caller owns the calendar.
func memberEmail(callerRole generated.CalendarMembersRole, callerID, memberID uint32, email string) string {
	if memberID == callerID || calresolve.CanSeeEmails(callerRole) {
		return email
	}
	return ""
}

func ListMembers(deps Deps) func(context.Context, *ListMembersInput) (*ListMembersOutput, error) {
	return func(ctx context.Context, in *ListMembersInput) (*ListMembersOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, caller, err := resolveCalendarMember(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		rows, err := deps.Queries.ListCalendarMembers(ctx, generated.ListCalendarMembersParams{
			CalendarID: cal.ID,
			Limit:      math.MaxInt32,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		out := &ListMembersOutput{Body: make([]MemberResponse, 0, len(rows))}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, m := range rows {
			out.Body = append(out.Body, MemberResponse{
				ID:        pubIDToHex(m.UserPublicID),
				Name:      m.UserDisplayName,
				Email:     memberEmail(caller.Role, userID, m.UserID, m.UserEmail),
				AvatarURL: av.FromKey(ctx, m.UserAvatarStorageKey, m.UserAvatarURL),
				Role:      string(m.Role),
				Color:     m.MemberColor,
			})
		}
		return out, nil
	}
}

// ListMemberCandidates lists the workspace members a manager can add to the
// calendar without sending a link.
func ListMemberCandidates(deps Deps) func(context.Context, *ListMemberCandidatesInput) (*ListMemberCandidatesOutput, error) {
	return func(ctx context.Context, in *ListMemberCandidatesInput) (*ListMemberCandidatesOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, caller, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		rows, err := deps.Queries.ListCalendarMemberCandidates(ctx, generated.ListCalendarMemberCandidatesParams{
			WorkspaceID: cal.WorkspaceID,
			CalendarID:  cal.ID,
			Query:       strings.TrimSpace(in.Query),
			Limit:       maxCandidatesListed + 1,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		out := &ListMemberCandidatesOutput{}
		if len(rows) > maxCandidatesListed {
			rows = rows[:maxCandidatesListed]
			out.Body.HasMore = true
		}
		out.Body.Items = make([]MemberCandidateResponse, 0, len(rows))
		av := avatars.New(deps.Queries, deps.Storage)
		// A candidate is never on the calendar, so never the caller.
		showEmails := calresolve.CanSeeEmails(caller.Role)
		for _, r := range rows {
			email := ""
			if showEmails {
				email = r.UserEmail
			}
			out.Body.Items = append(out.Body.Items, MemberCandidateResponse{
				ID:        pubIDToHex(r.UserPublicID),
				Name:      r.UserDisplayName,
				Email:     email,
				AvatarURL: av.FromKey(ctx, r.UserAvatarStorageKey, r.UserAvatarURL),
			})
		}
		return out, nil
	}
}

// AddMember grants a workspace member access to the calendar directly, the
// way a join link would once they opened it.
func AddMember(deps Deps) func(context.Context, *AddMemberInput) (*AddMemberOutput, error) {
	return func(ctx context.Context, in *AddMemberInput) (*AddMemberOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, actor, err := resolveCalendarManage(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		targetPub, err := parseUUID(in.Body.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		role := generated.CalendarMembersRole(in.Body.Role)

		var color string
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// The calendar is locked before either count is taken, so two
			// additions cannot both find room for the last place.
			if err := calresolve.LockExclusive(ctx, q, cal.ID); err != nil {
				return err
			}
			if err := calresolve.RequireRoomForMember(ctx, q, cal.ID); err != nil {
				return err
			}
			if err := calresolve.RequireRoomForCalendar(ctx, q, deps.WorkspaceID, target.ID); err != nil {
				return err
			}
			var err error
			color, err = calresolve.AddMember(ctx, q, cal, actorID, target, role)
			return err
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		return &AddMemberOutput{Body: MemberResponse{
			ID:        pubIDToHex(target.PublicID),
			Name:      target.DisplayName,
			Email:     memberEmail(actor.Role, actorID, target.ID, target.Email),
			AvatarURL: avatars.New(deps.Queries, deps.Storage).ForUser(ctx, target),
			Role:      string(role),
			Color:     color,
		}}, nil
	}
}

func UpdateMemberRole(deps Deps) func(context.Context, *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
	return func(ctx context.Context, in *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, _, err := resolveCalendarManage(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		// Managers change other members' roles, not their own. Changing your
		// own must go through somebody else.
		if target.ID == actorID {
			return nil, apierrors.ToHuma(apierrors.MemberSelfModify)
		}

		newRole := generated.CalendarMembersRole(in.Body.Role)

		var memberColor string
		var actorRole generated.CalendarMembersRole
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Both memberships are read under their row locks: the gates below
			// are decided on the roles the write then replaces, not on the
			// ones an earlier read saw.
			members, err := calresolve.LockMembers(ctx, q, cal.ID, actorID, target.ID)
			if err != nil {
				return err
			}
			actorMember, ok := members[actorID]
			if !ok {
				return apierrors.CalendarAccessDenied
			}
			if !calresolve.CanManage(actorMember.Role) {
				return apierrors.CalendarRoleRequired
			}
			actorRole = actorMember.Role
			current, ok := members[target.ID]
			if !ok {
				return apierrors.MemberNotFound
			}
			memberColor = current.MemberColor

			// Ownership only moves by the owner's hand. A manager that could hand
			// it to a second account it controls, or strip it from the person who
			// created the calendar, would hold every power the owner has plus
			// deniability -- so both directions are gated, not just the promotion.
			if (newRole == generated.CalendarMembersRoleOwner || current.Role == generated.CalendarMembersRoleOwner) &&
				!calresolve.CanOwn(actorMember.Role) {
				return apierrors.CalendarRoleRequired
			}

			// Lock the owner count and apply the role change atomically so
			// concurrent demotions cannot leave the calendar with nobody who
			// can administer it.
			if current.Role == generated.CalendarMembersRoleOwner && newRole != generated.CalendarMembersRoleOwner {
				ownerCount, err := q.CountCalendarOwnersForUpdate(ctx, cal.ID)
				if err != nil {
					return err
				}
				if ownerCount <= 1 {
					return apierrors.MemberLastAdmin
				}
			}
			if err := q.UpdateCalendarMemberRole(ctx, generated.UpdateCalendarMemberRoleParams{
				Role:       newRole,
				CalendarID: cal.ID,
				UserID:     target.ID,
			}); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: actorID,
				Type:        eventlog.TypeMemberRoleSet,
				Summary:     target.DisplayName + " -> " + in.Body.Role,
				Extra: map[string]any{
					"calendarId": eventlog.UUID(cal.PublicID),
					"userId":     eventlog.UUID(target.PublicID),
					"newRole":    in.Body.Role,
				},
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		return &UpdateMemberRoleOutput{Body: MemberResponse{
			ID:        pubIDToHex(target.PublicID),
			Name:      target.DisplayName,
			Email:     memberEmail(actorRole, actorID, target.ID, target.Email),
			AvatarURL: avatars.New(deps.Queries, deps.Storage).ForUser(ctx, target),
			Role:      in.Body.Role,
			Color:     memberColor,
		}}, nil
	}
}

// UpdateMemberColor changes the colour a member's layer is drawn in.
//
// The product is one shared calendar with a colour per person, so this is not
// a cosmetic preference: it is how anyone reading the calendar tells whose
// plan is whose. It belongs to the calendar rather than the viewer, which is
// why it is changed here and not in a local setting.
func UpdateMemberColor(deps Deps) func(context.Context, *UpdateMemberColorInput) (*UpdateMemberColorOutput, error) {
	return func(ctx context.Context, in *UpdateMemberColorInput) (*UpdateMemberColorOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, _, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		var actorMember, current generated.CalendarMember
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			members, err := calresolve.LockMembers(ctx, q, cal.ID, actorID, target.ID)
			if err != nil {
				return err
			}
			var ok bool
			if actorMember, ok = members[actorID]; !ok {
				return apierrors.CalendarAccessDenied
			}
			// Your own colour is yours to pick; anyone else's is the calendar's
			// administration, because two members claiming the same colour is a
			// problem only somebody looking at the whole list can resolve.
			if target.ID != actorID && !calresolve.CanManage(actorMember.Role) {
				return apierrors.CalendarRoleRequired
			}
			if current, ok = members[target.ID]; !ok {
				return apierrors.MemberNotFound
			}
			if err := q.UpdateCalendarMemberColor(ctx, generated.UpdateCalendarMemberColorParams{
				MemberColor: in.Body.Color,
				CalendarID:  cal.ID,
				UserID:      target.ID,
			}); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: actorID,
				Type:        eventlog.TypeMemberColorSet,
				Summary:     target.DisplayName,
				Extra: map[string]any{
					"calendarId": eventlog.UUID(cal.PublicID),
					"userId":     eventlog.UUID(target.PublicID),
				},
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		return &UpdateMemberColorOutput{Body: MemberResponse{
			ID:        pubIDToHex(target.PublicID),
			Name:      target.DisplayName,
			Email:     memberEmail(actorMember.Role, actorID, target.ID, target.Email),
			AvatarURL: avatars.New(deps.Queries, deps.Storage).ForUser(ctx, target),
			Role:      string(current.Role),
			Color:     in.Body.Color,
		}}, nil
	}
}

func RemoveMember(deps Deps) func(context.Context, *RemoveMemberInput) (*RemoveMemberOutput, error) {
	return func(ctx context.Context, in *RemoveMemberInput) (*RemoveMemberOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, actorMember, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		actor, err := deps.Queries.GetUserByID(ctx, actorID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		// Compare parsed UUID bytes so a self-leave is recognized regardless of the
		// hex casing the client used in the path.
		isSelfLeave := bytes.Equal(targetPub, actor.PublicID)
		// A caller who may not remove anyone is refused before the target's
		// existence is revealed; the role is checked again under lock below.
		if !isSelfLeave && !calresolve.CanManage(actorMember.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		// Leaving and being removed share one kind; a member who chose to go
		// is told apart by reason "left", since only one of the two is
		// something the member chose.
		removedExtra := map[string]any{
			"calendarId": eventlog.UUID(cal.PublicID),
			"userId":     eventlog.UUID(target.PublicID),
		}
		if isSelfLeave {
			removedExtra["reason"] = "left"
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// The roles every gate below decides on are read here, under their
			// row locks, so a concurrent promotion or demotion is seen.
			members, err := calresolve.LockMembers(ctx, q, cal.ID, actorID, target.ID)
			if err != nil {
				return err
			}
			actorMember, ok := members[actorID]
			if !ok {
				return apierrors.CalendarAccessDenied
			}
			if !isSelfLeave && !calresolve.CanManage(actorMember.Role) {
				return apierrors.CalendarRoleRequired
			}
			current, ok := members[target.ID]
			if !ok {
				return apierrors.MemberNotFound
			}

			// Removing an owner is the same power as demoting one, reached by a
			// different verb. Gating only the role change would leave the shorter
			// path open. Leaving of your own accord stays available to anyone.
			if !isSelfLeave && current.Role == generated.CalendarMembersRoleOwner &&
				!calresolve.CanOwn(actorMember.Role) {
				return apierrors.CalendarRoleRequired
			}

			// Lock the owner count and revoke atomically so concurrent
			// removals cannot leave the calendar without an owner.
			if current.Role == generated.CalendarMembersRoleOwner {
				ownerCount, err := q.CountCalendarOwnersForUpdate(ctx, cal.ID)
				if err != nil {
					return err
				}
				if ownerCount <= 1 {
					return apierrors.MemberLastAdmin
				}
			}
			if err := q.RevokeCalendarMember(ctx, generated.RevokeCalendarMemberParams{
				CalendarID: cal.ID,
				UserID:     target.ID,
			}); err != nil {
				return err
			}
			if err := releaseMemberEvents(ctx, q, deps.WorkspaceID, cal.ID, actorID, target.ID); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: actorID,
				Type:        eventlog.TypeMemberRemoved,
				Summary:     target.DisplayName,
				Extra:       removedExtra,
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &RemoveMemberOutput{}, nil
	}
}

// releaseMemberEvents detaches a member who has just been revoked from the
// calendar's events: their attendance goes, and the events they owned pass to
// the longest-standing owner. Every event a write accepts names members only,
// so leaving either behind would make the event impossible to save as read.
// The series touched get a new revision, so a copy read before the departure
// is refused as stale rather than as invalid.
func releaseMemberEvents(ctx context.Context, q *generated.Queries, workspaceID, calendarID, actorID, userID uint32) error {
	series, err := q.ListSeriesTouchingMember(ctx, generated.ListSeriesTouchingMemberParams{
		UserID:     userID,
		CalendarID: calendarID,
	})
	if err != nil || len(series) == 0 {
		return err
	}
	heir, err := q.GetLongestStandingOwner(ctx, calendarID)
	if err != nil {
		return err
	}
	// Read before the hand-over: afterwards nothing names the previous owner.
	handedOver, err := q.ListEventsOwnedBy(ctx, generated.ListEventsOwnedByParams{
		CalendarID:  calendarID,
		OwnerUserID: userID,
	})
	if err != nil {
		return err
	}
	if err := q.ReassignCalendarEventOwner(ctx, generated.ReassignCalendarEventOwnerParams{
		NewOwnerID: heir,
		CalendarID: calendarID,
		OldOwnerID: userID,
	}); err != nil {
		return err
	}
	if err := q.DisableCalendarAttendee(ctx, generated.DisableCalendarAttendeeParams{
		UserID:     userID,
		CalendarID: calendarID,
	}); err != nil {
		return err
	}
	if err := q.TouchCalendarEvents(ctx, series); err != nil {
		return err
	}
	return logOwnerReassigned(ctx, q, workspaceID, calendarID, actorID, handedOver)
}

// logOwnerReassigned records, for each event that changed hands, that its
// owner changed, so the event's history shows why its owner is someone else.
func logOwnerReassigned(
	ctx context.Context, q *generated.Queries, workspaceID, calendarID, actorID uint32,
	events []generated.ListEventsOwnedByRow,
) error {
	for _, e := range events {
		if err := eventlog.Append(ctx, q, eventlog.Entry{
			WorkspaceID: workspaceID,
			CalendarID:  calendarID,
			ActorUserID: actorID,
			Type:        eventlog.TypeEventUpdated,
			Title:       e.Title,
			Subject:     e.PublicID,
			Extra:       map[string]any{"changes": []string{"owner"}},
		}); err != nil {
			return err
		}
	}
	return nil
}
