package events

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// defaultCommentPageSize and maxCommentPageSize bound one page of a thread.
const (
	defaultCommentPageSize = 50
	maxCommentPageSize     = 200
)

// commentListRow is the shape both comment listings share, so one mapping
// serves the newest page and the pages behind it.
type commentListRow struct {
	id                   uint32
	publicID             []byte
	userPublicID         []byte
	userName             string
	userAvatarURL        sql.NullString
	userAvatarStorageKey sql.NullString
	body                 string
	updatedAt            sql.NullTime
	createdAt            time.Time
}

// resolveCommentCursor turns a cursor back into the ordering pair it names.
// It is looked up within the event the caller has already proved access to,
// so a comment id from elsewhere does not resolve; a comment removed since the
// cursor was issued still does, because its position in the thread has not
// moved.
func resolveCommentCursor(ctx context.Context, deps Deps, eventID uint32, cursor string) (time.Time, uint32, error) {
	pub, err := parseUUID(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	c, err := deps.Queries.GetEventCommentCursor(ctx, generated.GetEventCommentCursorParams{
		PublicID: pub,
		EventID:  sql.NullInt32{Int32: int32(eventID), Valid: true},
	})
	if err != nil {
		return time.Time{}, 0, err
	}
	return c.CreatedAt, c.ID, nil
}

func ListComments(deps Deps) func(context.Context, *ListCommentsInput) (*ListCommentsOutput, error) {
	return func(ctx context.Context, in *ListCommentsInput) (*ListCommentsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, evt, err := readableSubresourceEvent(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		limit := int32(in.Limit)
		if limit <= 0 {
			limit = defaultCommentPageSize
		}
		if limit > maxCommentPageSize {
			limit = maxCommentPageSize
		}
		// One extra row is what says whether there is more thread behind this
		// page, without counting the whole of it.
		fetchLimit := limit + 1

		eventID := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
		var rows []commentListRow
		if in.Cursor == "" {
			latest, qerr := deps.Queries.ListEventCommentsLatest(ctx, generated.ListEventCommentsLatestParams{
				WorkspaceID: deps.WorkspaceID,
				EventID:     eventID,
				Limit:       fetchLimit,
			})
			err = qerr
			rows = make([]commentListRow, 0, len(latest))
			for _, c := range latest {
				rows = append(rows, commentListRow{
					id: c.ID, publicID: c.PublicID, userPublicID: c.UserPublicID,
					userName: c.UserDisplayName, userAvatarURL: c.UserAvatarURL,
					userAvatarStorageKey: c.UserAvatarStorageKey,
					body:                 c.Body, updatedAt: c.UpdatedAt, createdAt: c.CreatedAt,
				})
			}
		} else {
			before, beforeID, cerr := resolveCommentCursor(ctx, deps, evt.ID, in.Cursor)
			if cerr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			older, qerr := deps.Queries.ListEventCommentsBefore(ctx, generated.ListEventCommentsBeforeParams{
				WorkspaceID:     deps.WorkspaceID,
				EventID:         eventID,
				BeforeCreatedAt: before,
				BeforeID:        beforeID,
				Limit:           fetchLimit,
			})
			err = qerr
			rows = make([]commentListRow, 0, len(older))
			for _, c := range older {
				rows = append(rows, commentListRow{
					id: c.ID, publicID: c.PublicID, userPublicID: c.UserPublicID,
					userName: c.UserDisplayName, userAvatarURL: c.UserAvatarURL,
					userAvatarStorageKey: c.UserAvatarStorageKey,
					body:                 c.Body, updatedAt: c.UpdatedAt, createdAt: c.CreatedAt,
				})
			}
		}
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		hasMore := int32(len(rows)) > limit
		if hasMore {
			rows = rows[:limit]
		}
		// The rows arrive newest first, because that is the end of a thread
		// worth bounding. A page is read oldest first, so the cursor comes off
		// the last row before the page is turned round.
		nextCursor := ""
		if hasMore && len(rows) > 0 {
			nextCursor = pubIDToHex(rows[len(rows)-1].publicID)
		}
		slices.Reverse(rows)

		out := &ListCommentsOutput{Body: ListCommentsPage{
			Items:      make([]CommentResponse, 0, len(rows)),
			NextCursor: nextCursor,
		}}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, c := range rows {
			out.Body.Items = append(out.Body.Items, CommentResponse{
				ID:              pubIDToHex(c.publicID),
				AuthorID:        pubIDToHex(c.userPublicID),
				AuthorName:      c.userName,
				AuthorAvatarURL: av.FromKey(ctx, c.userAvatarStorageKey, c.userAvatarURL),
				Body:            c.body,
				ETag:            rowETag(c.updatedAt, c.createdAt),
				CreatedAt:       c.createdAt,
			})
		}
		return out, nil
	}
}

func CreateComment(deps Deps) func(context.Context, *CreateCommentInput) (*CreateCommentOutput, error) {
	return func(ctx context.Context, in *CreateCommentInput) (*CreateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		var stored generated.GetEventCommentByPublicIDRow
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			if _, err := q.CreateEventComment(ctx, generated.CreateEventCommentParams{
				PublicID:    pubID[:],
				WorkspaceID: deps.WorkspaceID,
				EventID:     sql.NullInt32{Int32: int32(w.evt.ID), Valid: true},
				AuthorID:    userID,
				Body:        in.Body.Body,
			}); err != nil {
				return err
			}
			if err := logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentCreated, pubID[:]); err != nil {
				return err
			}
			// Read back so createdAt is the stored value rather than a guess at it.
			stored, err = q.GetEventCommentByPublicID(ctx, pubID[:])
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		resp := mapComment(ctx, deps, stored)
		return &CreateCommentOutput{ETag: resp.ETag, Body: resp}, nil
	}
}

// mapComment renders a stored comment with its author.
func mapComment(ctx context.Context, deps Deps, c generated.GetEventCommentByPublicIDRow) CommentResponse {
	return CommentResponse{
		ID:              pubIDToHex(c.PublicID),
		AuthorID:        pubIDToHex(c.UserPublicID),
		AuthorName:      c.UserDisplayName,
		AuthorAvatarURL: avatars.New(deps.Queries, deps.Storage).FromKey(ctx, c.UserAvatarStorageKey, c.UserAvatarURL),
		Body:            c.Body,
		ETag:            rowETag(c.UpdatedAt, c.CreatedAt),
		CreatedAt:       c.CreatedAt,
	}
}

// subresourceVisible reports whether the member may see the comments,
// checklist and attachments of the event. They hang off the series, so the
// series row and its own attendees decide.
func subresourceVisible(ctx context.Context, q *generated.Queries, cal generated.Calendar, evt generated.CalendarEvent, userID uint32) (bool, error) {
	attendees, err := q.ListEnabledEventAttendeeIDs(ctx, sql.NullInt32{Int32: int32(evt.ID), Valid: true})
	if err != nil {
		return false, err
	}
	d := eventacl.Decide(eventacl.Event{
		Visibility:      eventacl.Visibility(evt.Visibility),
		CalendarDefault: eventacl.Visibility(cal.DefaultEventVisibility),
		OwnerUserID:     evt.OwnerUserID,
		CreatedByUserID: evt.CreatedByUserID,
	}, eventacl.Reader{UserID: userID, IsMember: true}, attendees)
	return d.Subresource, nil
}

// readableSubresourceEvent resolves the event whose comments, checklist or
// attachments a member reads. An event the member cannot see the details of
// does not exist for them: it answers as not found.
func readableSubresourceEvent(ctx context.Context, deps Deps, calPubID, eventID string, userID uint32) (generated.Calendar, generated.CalendarEvent, error) {
	cal, err := resolveCalendar(ctx, deps, calPubID, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	evt, err := resolveCommentEvent(ctx, deps, cal.ID, eventID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	visible, err := subresourceVisible(ctx, deps.Queries, cal, evt, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	if !visible {
		return generated.Calendar{}, generated.CalendarEvent{}, apierrors.EventNotFound
	}
	return cal, evt, nil
}

// subresourceWriteTarget is eventWriteTarget for a write to a row hanging off
// an event. It gates on membership only: a member for whom the event does not
// exist is told so before being told their role could not write, and that
// is decided in the transaction by lockSubresourceAccess.
func subresourceWriteTarget(ctx context.Context, deps Deps, calPubID, eventID string, userID uint32) (generated.Calendar, generated.CalendarEvent, string, error) {
	return eventTarget(ctx, deps, calPubID, eventID, userID, resolveCalendar)
}

// lockSubresourceAccess is lockEventAccess followed by the visibility check: an
// event whose sub-resources the member may not see is not found.
func lockSubresourceAccess(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockEventAccess(ctx, q, cal, eventID, userID, "", scopeAll)
	if err != nil {
		return eventAccess{}, err
	}
	visible, err := subresourceVisible(ctx, q, w.cal, w.evt, userID)
	if err != nil {
		return eventAccess{}, err
	}
	if !visible {
		return eventAccess{}, apierrors.EventNotFound
	}
	return w, nil
}

// lockDetailEdit is lockEventEdit for a row hanging off the event: checklist
// items and attachments need the right to change the event's fields.
func lockDetailEdit(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockSubresourceAccess(ctx, q, cal, eventID, userID)
	if err != nil {
		return eventAccess{}, err
	}
	if err := requireEventEdit(ctx, q, w, userID, editFields); err != nil {
		return eventAccess{}, err
	}
	return w, nil
}

// lockCommentWrite is lockSubresourceAccess for a comment: anyone who may write
// to the calendar may comment on any event they can see, so the role is the
// whole check.
func lockCommentWrite(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockSubresourceAccess(ctx, q, cal, eventID, userID)
	if err != nil {
		return eventAccess{}, err
	}
	if !calresolve.CanWrite(w.member.Role) {
		return eventAccess{}, apierrors.CalendarRoleRequired
	}
	return w, nil
}

// lockComment reads one live comment of the event under its row lock.
func lockComment(ctx context.Context, q *generated.Queries, eventID uint32, commentID string) (generated.CalendarEventComment, error) {
	pub, err := parseUUID(commentID)
	if err != nil {
		return generated.CalendarEventComment{}, apierrors.CommentNotFound
	}
	c, err := q.GetEventCommentForUpdate(ctx, generated.GetEventCommentForUpdateParams{
		PublicID: pub,
		EventID:  sql.NullInt32{Int32: int32(eventID), Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarEventComment{}, apierrors.CommentNotFound
	}
	return c, err
}

// logCommentActivity records a comment being added, edited or removed. The
// entry names the event and the comment but carries none of its text: the log
// is append-only and readable by every member, so text written into it would
// outlive the edit that replaced it and the removal that took it down.
func logCommentActivity(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess, eventType string, commentPubID []byte) error {
	return logEventActivity(ctx, q, deps, w.cal.ID, actorID, w.evt, eventType, "commentId", "", commentPubID)
}

func resolveCommentEvent(ctx context.Context, deps Deps, calID uint32, eventID string) (generated.CalendarEvent, error) {
	// Comments hang off the series, not an individual occurrence: a
	// composite id resolves to its parent.
	if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
		eventID = parentUUID
	}
	return loadEventInCalendar(ctx, deps, calID, eventID)
}

func UpdateComment(deps Deps) func(context.Context, *UpdateCommentInput) (*UpdateCommentOutput, error) {
	return func(ctx context.Context, in *UpdateCommentInput) (*UpdateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		text, hasText := in.Body.Body.Get()
		if in.Body.Body.IsNull() {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var stored generated.GetEventCommentByPublicIDRow
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			comment, err := lockComment(ctx, q, w.evt.ID, in.CommentID)
			if err != nil {
				return err
			}
			if comment.AuthorID != userID {
				return apierrors.CommentAccessDenied
			}
			if err := checkIfMatch(in.IfMatch, rowETag(comment.UpdatedAt, comment.CreatedAt), apierrors.CommentStale); err != nil {
				return err
			}
			if hasText {
				if err := q.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
					Body: text,
					ID:   comment.ID,
				}); err != nil {
					return err
				}
				if err := logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentUpdated, comment.PublicID); err != nil {
					return err
				}
			}
			stored, err = q.GetEventCommentByPublicID(ctx, comment.PublicID)
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		resp := mapComment(ctx, deps, stored)
		return &UpdateCommentOutput{ETag: resp.ETag, Body: resp}, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			comment, err := lockComment(ctx, q, w.evt.ID, in.CommentID)
			if err != nil {
				return err
			}
			// Removing a comment is moderation as well as authorship: whoever
			// administers a shared calendar has to be able to take down what
			// someone else posted on it. Editing stays the author's alone -- the
			// two are not the same power, since an edit puts words in someone's
			// mouth while a removal only takes them off the wall.
			if comment.AuthorID != userID && !calresolve.CanManage(w.member.Role) {
				return apierrors.CommentAccessDenied
			}
			if err := checkIfMatch(in.IfMatch, rowETag(comment.UpdatedAt, comment.CreatedAt), apierrors.CommentStale); err != nil {
				return err
			}
			res, err := q.SoftDeleteEventComment(ctx, comment.ID)
			if err != nil {
				return err
			}
			removed, err := blobref.ChangedOne(res)
			if err != nil {
				return err
			}
			if !removed {
				return apierrors.CommentNotFound
			}
			return logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentDeleted, comment.PublicID)
		}); err != nil {
			return nil, toAPIError(err)
		}
		return &DeleteCommentOutput{}, nil
	}
}
