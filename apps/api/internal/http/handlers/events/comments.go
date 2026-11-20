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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
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
					body:                 c.Body, createdAt: c.CreatedAt,
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
					body:                 c.Body, createdAt: c.CreatedAt,
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
				ID:           pubIDToHex(c.publicID),
				UserPublicID: pubIDToHex(c.userPublicID),
				UserName:     c.userName,
				UserAvatar:   av.FromKey(ctx, c.userAvatarStorageKey, c.userAvatarURL),
				Body:         c.body,
				CreatedAt:    c.createdAt,
			})
		}
		return out, nil
	}
}

func CreateComment(deps Deps) func(context.Context, *CreateCommentInput) (*CreateCommentOutput, error) {
	return func(ctx context.Context, in *CreateCommentInput) (*CreateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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
				Body:        in.Body.Content,
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

		return &CreateCommentOutput{Body: mapComment(ctx, deps, stored)}, nil
	}
}

// mapComment renders a stored comment with its author.
func mapComment(ctx context.Context, deps Deps, c generated.GetEventCommentByPublicIDRow) CommentResponse {
	return CommentResponse{
		ID:           pubIDToHex(c.PublicID),
		UserPublicID: pubIDToHex(c.UserPublicID),
		UserName:     c.UserDisplayName,
		UserAvatar:   avatars.New(deps.Queries, deps.Storage).FromKey(ctx, c.UserAvatarStorageKey, c.UserAvatarURL),
		Body:         c.Body,
		CreatedAt:    c.CreatedAt,
	}
}

// lockCommentWrite is lockEventAccess for a comment: anyone who may write to
// the calendar may comment on any event in it, so the role is the whole check.
func lockCommentWrite(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockEventAccess(ctx, q, cal, eventID, userID, "", scopeAll)
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
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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
			if err := q.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
				Body: in.Body.Content,
				ID:   comment.ID,
			}); err != nil {
				return err
			}
			if err := logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentUpdated, comment.PublicID); err != nil {
				return err
			}
			stored, err = q.GetEventCommentByPublicID(ctx, comment.PublicID)
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &UpdateCommentOutput{Body: mapComment(ctx, deps, stored)}, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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
