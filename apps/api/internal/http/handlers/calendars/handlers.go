package calendars

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	Storage     *storage.Client
	WorkspaceID uint32
}

// defaultCalendarColor is applied when a client sends no colour.
const defaultCalendarColor = "#4CAF50"

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

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func mapCalendar(c generated.Calendar) CalendarResponse {
	return CalendarResponse{
		ID:        pubIDToHex(c.PublicID),
		Name:      c.Name,
		Color:     c.Color,
		CoverURL:  nullStringValue(c.CoverURL),
		CreatedAt: c.CreatedAt,
	}
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
func resolveCalendarManage(ctx context.Context, deps Deps, calendarPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, calendarPubID, userID)
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
			resp := CalendarResponse{
				ID:        pubIDToHex(c.PublicID),
				Name:      c.Name,
				Color:     c.Color,
				CoverURL:  nullStringValue(c.CoverURL),
				CreatedAt: c.CreatedAt,
			}
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
			return nil, toAPIError(err)
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
		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		memberPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		color := in.Body.Color
		if color == "" {
			color = defaultCalendarColor
		}

		var created generated.Calendar
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// owner_user_id is deliberately left NULL. The column cascades on
			// delete, so naming an owner would mean removing that one person
			// deletes the calendar and every event in it -- which is the
			// opposite of what a calendar a group shares is for. Who may
			// administer it is a calendar_members role, not this column.
			result, err := q.CreateCalendar(ctx, generated.CreateCalendarParams{
				PublicID:    pubID[:],
				WorkspaceID: deps.WorkspaceID,
				Name:        in.Body.Name,
				Color:       color,
			})
			if err != nil {
				return err
			}
			calID64, err := result.LastInsertId()
			if err != nil {
				return err
			}
			calID := uint32(calID64)

			// The creator becomes the owning member, which is what actually
			// grants them administration.
			if _, err := q.AddCalendarMember(ctx, generated.AddCalendarMemberParams{
				PublicID:    memberPubID[:],
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  calID,
				UserID:      userID,
				Role:        generated.CalendarMembersRoleOwner,
				MemberColor: color,
			}); err != nil {
				return err
			}

			created, err = q.GetCalendarByID(ctx, calID)
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  calID,
				ActorUserID: userID,
				Type:        eventlog.TypeCalendarSetUp,
				Summary:     in.Body.Name,
				Subject:     pubID[:],
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &CreateCalendarOutput{Body: mapCalendar(created)}, nil
	}
}

func UpdateCalendar(deps Deps) func(context.Context, *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
	return func(ctx context.Context, in *UpdateCalendarInput) (*UpdateCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarManage(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// All fields are optional: when omitted, keep the current values so a
		// partial update does not blank out other fields.
		name := cal.Name
		if in.Body.Name != nil {
			name = *in.Body.Name
		}
		color := cal.Color
		if in.Body.Color != nil {
			color = *in.Body.Color
		}
		coverURL := cal.CoverURL
		if in.Body.CoverURL != nil {
			coverURL = nullString(*in.Body.CoverURL)
		}

		var updated generated.Calendar
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdateCalendar(ctx, generated.UpdateCalendarParams{
				Name:     name,
				Color:    color,
				CoverURL: coverURL,
				ID:       cal.ID,
			}); err != nil {
				return err
			}
			var err error
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
				Subject:     cal.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &UpdateCalendarOutput{Body: mapCalendar(updated)}, nil
	}
}

func DeleteCalendar(deps Deps) func(context.Context, *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
	return func(ctx context.Context, in *DeleteCalendarInput) (*DeleteCalendarOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarOwn(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Release the blobs this calendar's attachments were holding.
			// The objects are content-addressed and may be shared, so they
			// are not deleted here -- dropping the reference is what lets the
			// sweep collect the ones nothing else points at.
			//
			// The rows are retired in the same transaction so the release
			// happens once: only live rows are counted, so deleting the
			// calendar twice cannot drive a shared object's count below what
			// other calendars still hold.
			objectIDs, err := q.ListAttachmentObjectIDsByCalendar(ctx, cal.ID)
			if err != nil {
				return err
			}
			for _, objectID := range objectIDs {
				if err := q.DecrementStorageObjectRefs(ctx, objectID); err != nil {
					return err
				}
			}
			if err := q.SoftDeleteAttachmentsByCalendar(ctx, cal.ID); err != nil {
				return err
			}
			// Album photos go the same way. Deleting the calendar only disables
			// the calendar row, so no cascade reaches them: left enabled they
			// stay in object storage for good, with no API path left that could
			// ever name them again.
			if err := q.SoftDeleteAlbumPhotosByCalendar(ctx, cal.ID); err != nil {
				return err
			}
			if err := q.SoftDeleteCalendar(ctx, cal.ID); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeCalendarGone,
				Summary:     cal.Name,
				Subject:     cal.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteCalendarOutput{}, nil
	}
}

func ListMembers(deps Deps) func(context.Context, *ListMembersInput) (*ListMembersOutput, error) {
	return func(ctx context.Context, in *ListMembersInput) (*ListMembersOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, caller, err := resolveCalendarMember(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListCalendarMembers(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListMembersOutput{Body: make([]MemberResponse, 0, len(rows))}
		for _, m := range rows {
			// An address is shown to whoever administers the calendar and to
			// its owner; to everyone else a member is a name and a colour.
			email := ""
			if calresolve.CanManage(caller.Role) || m.UserID == userID {
				email = m.UserEmail
			}
			out.Body = append(out.Body, MemberResponse{
				ID:     pubIDToHex(m.UserPublicID),
				Name:   m.UserDisplayName,
				Email:  email,
				Avatar: nullStringValue(m.UserAvatarURL),
				Role:   string(m.Role),
				Color:  m.MemberColor,
			})
		}
		return out, nil
	}
}

func UpdateMemberRole(deps Deps) func(context.Context, *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
	return func(ctx context.Context, in *UpdateMemberRoleInput) (*UpdateMemberRoleOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, actorMember, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if !calresolve.CanManage(actorMember.Role) {
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
		// Managers change other members' roles, not their own. Changing your
		// own must go through somebody else.
		if target.ID == actorID {
			return nil, apierrors.ToHuma(apierrors.MemberSelfModify)
		}
		current, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: target.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		newRole := generated.CalendarMembersRole(in.Body.Role)

		// Ownership only moves by the owner's hand. A manager that could hand
		// it to a second account it controls, or strip it from the person who
		// created the calendar, would hold every power the owner has plus
		// deniability -- so both directions are gated, not just the promotion.
		if (newRole == generated.CalendarMembersRoleOwner || current.Role == generated.CalendarMembersRoleOwner) &&
			!calresolve.CanOwn(actorMember.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
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
				Subject:     target.PublicID,
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &UpdateMemberRoleOutput{Body: MemberResponse{
			ID:     pubIDToHex(target.PublicID),
			Name:   target.DisplayName,
			Email:  target.Email,
			Avatar: nullStringValue(target.AvatarURL),
			Role:   in.Body.Role,
			Color:  current.MemberColor,
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
		cal, actorMember, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, toAPIError(err)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		// Your own colour is yours to pick; anyone else's is the calendar's
		// administration, because two members claiming the same colour is a
		// problem only somebody looking at the whole list can resolve.
		if target.ID != actorID && !calresolve.CanManage(actorMember.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}
		current, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: target.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		if err := deps.Queries.UpdateCalendarMemberColor(ctx, generated.UpdateCalendarMemberColorParams{
			MemberColor: in.Body.Color,
			CalendarID:  cal.ID,
			UserID:      target.ID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		email := ""
		if calresolve.CanManage(actorMember.Role) || target.ID == actorID {
			email = target.Email
		}
		return &UpdateMemberColorOutput{Body: MemberResponse{
			ID:     pubIDToHex(target.PublicID),
			Name:   target.DisplayName,
			Email:  email,
			Avatar: nullStringValue(target.AvatarURL),
			Role:   string(current.Role),
			Color:  in.Body.Color,
		}}, nil
	}
}

func RemoveMember(deps Deps) func(context.Context, *RemoveMemberInput) (*RemoveMemberOutput, error) {
	return func(ctx context.Context, in *RemoveMemberInput) (*RemoveMemberOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		cal, actorMember, err := resolveCalendarMember(ctx, deps, in.CalendarID, actorID)
		if err != nil {
			return nil, toAPIError(err)
		}

		actor, err := deps.Queries.GetUserByID(ctx, actorID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		targetPub, err := parseUUID(in.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		// Compare parsed UUID bytes so a self-leave is recognized regardless of the
		// hex casing the client used in the path.
		isSelfLeave := bytes.Equal(targetPub, actor.PublicID)
		if !isSelfLeave && !calresolve.CanManage(actorMember.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}
		target, err := deps.Queries.GetUserByPublicID(ctx, targetPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}
		current, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: cal.ID, UserID: target.ID})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		if isSelfLeave && target.ID != actorID {
			return nil, apierrors.ToHuma(apierrors.MemberNotFound)
		}

		// Removing an owner is the same power as demoting one, reached by a
		// different verb. Gating only the role change would leave the shorter
		// path open. Leaving of your own accord stays available to anyone.
		if !isSelfLeave && current.Role == generated.CalendarMembersRoleOwner &&
			!calresolve.CanOwn(actorMember.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
		}

		// Leaving and being removed are different events, not one event with
		// a flag: a feed that cannot tell them apart reads the same either
		// way, and only one of the two is something the member chose.
		eventType := eventlog.TypeMemberRemoved
		if isSelfLeave {
			eventType = eventlog.TypeMemberLeft
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
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
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: actorID,
				Type:        eventType,
				Summary:     target.DisplayName,
				Subject:     target.PublicID,
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}
		return &RemoveMemberOutput{}, nil
	}
}

// labelPalette builds the colour list, one entry per colour, with the name as
// an i18n key so the client can localize it.
func labelPalette() []LabelResponse {
	colors := []string{
		"#47B2F7", "#F35F8C", "#B38BDC", "#FDC02D", "#E73B3B",
		"#2ECC87", "#F5A623", "#8F8F8F", "#42A5F5", "#FF7043",
	}
	labels := make([]LabelResponse, len(colors))
	for i, c := range colors {
		id := strconv.Itoa(i + 1)
		labels[i] = LabelResponse{ID: id, NameKey: "label." + id, Color: c}
	}
	return labels
}

// ListLabels returns the predefined color palette. Names are returned as i18n
// keys (label.1 .. label.10) so the frontend can localize them.
//
// The same list is repeated in the web client's MEMBER_COLORS, because the
// first calendar is created before there is one to ask. Both sides are pinned
// by a test: a palette that drifts hands out a colour the calendar's own list
// does not contain.
func ListLabels(deps Deps) func(context.Context, *ListLabelsInput) (*ListLabelsOutput, error) {
	labels := labelPalette()
	return func(ctx context.Context, in *ListLabelsInput) (*ListLabelsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		if _, err := resolveCalendar(ctx, deps, in.CalendarID, userID); err != nil {
			return nil, toAPIError(err)
		}
		return &ListLabelsOutput{Body: labels}, nil
	}
}
