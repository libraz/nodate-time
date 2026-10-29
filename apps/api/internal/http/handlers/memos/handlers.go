package memos

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	WorkspaceID uint32
}

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func toAPIError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
}

func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Write(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
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

func nullTimeValue(n sql.NullTime) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return n.Time
}

func mapMemo(m generated.CalendarMemo) MemoResponse {
	return MemoResponse{
		ID:        pubIDToHex(m.PublicID),
		Title:     m.Title,
		Body:      nullStringValue(m.Body),
		Done:      m.Done,
		SortOrder: m.SortWeight,
		CreatedAt: m.CreatedAt,
		UpdatedAt: nullTimeValue(m.UpdatedAt),
	}
}

// lockMemo reads a memo for a write, under its row lock, inside the
// transaction that commits the write. An id from another calendar cannot be
// reached by guessing, and a memo deleted in the meantime is not found.
func lockMemo(ctx context.Context, q *generated.Queries, calID uint32, memoID string) (generated.CalendarMemo, error) {
	memoPub, err := parseUUID(memoID)
	if err != nil {
		return generated.CalendarMemo{}, apierrors.MemoNotFound
	}
	memo, err := q.GetMemoForUpdate(ctx, generated.GetMemoForUpdateParams{
		PublicID:   memoPub,
		CalendarID: calID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarMemo{}, apierrors.MemoNotFound
	}
	return memo, err
}

// defaultMemoPageSize and maxMemoPageSize bound one page of memos.
const (
	defaultMemoPageSize = 100
	maxMemoPageSize     = 200
)

// encodeMemoCursor names the last memo of a page by its public id.
//
// Resuming needs the whole ordering triple, but two of its parts are the
// list's internal arrangement and the row's internal id, and a client is
// given neither. Naming the row lets the server look the triple up again and
// tells the holder only which memo they have already seen.
func encodeMemoCursor(publicID []byte) string {
	return pubIDToHex(publicID)
}

// resolveMemoCursor turns a cursor back into the ordering triple it names. A
// cursor from another calendar does not resolve here, so it is refused rather
// than silently paging through this one from an unrelated position. A memo
// deleted since the page was issued still resolves: it has an ordering
// position, and the list being walked must not break under it.
func resolveMemoCursor(ctx context.Context, deps Deps, calID uint32, cursor string) (generated.CalendarMemo, error) {
	pub, err := parseUUID(cursor)
	if err != nil {
		return generated.CalendarMemo{}, err
	}
	return deps.Queries.GetMemoCursor(ctx, generated.GetMemoCursorParams{
		PublicID:   pub,
		CalendarID: calID,
	})
}

func ListMemos(deps Deps) func(context.Context, *ListMemosInput) (*ListMemosOutput, error) {
	return func(ctx context.Context, in *ListMemosInput) (*ListMemosOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		limit := int32(in.Limit)
		if limit <= 0 {
			limit = defaultMemoPageSize
		}
		if limit > maxMemoPageSize {
			limit = maxMemoPageSize
		}
		// One extra row is what says whether there is a page after this one,
		// without a second count of the whole list.
		fetchLimit := limit + 1

		var rows []generated.CalendarMemo
		if in.Cursor == "" {
			rows, err = deps.Queries.ListMemosByCalendarFirstPage(ctx, generated.ListMemosByCalendarFirstPageParams{
				CalendarID: cal.ID,
				Limit:      fetchLimit,
			})
		} else {
			after, cerr := resolveMemoCursor(ctx, deps, cal.ID, in.Cursor)
			if cerr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			rows, err = deps.Queries.ListMemosByCalendarAfter(ctx, generated.ListMemosByCalendarAfterParams{
				CalendarID:      cal.ID,
				AfterSortWeight: after.SortWeight,
				AfterCreatedAt:  after.CreatedAt,
				AfterID:         after.ID,
				Limit:           fetchLimit,
			})
		}
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		hasMore := int32(len(rows)) > limit
		if hasMore {
			rows = rows[:limit]
		}

		out := &ListMemosOutput{Body: ListMemosPage{Items: make([]MemoResponse, 0, len(rows))}}
		for _, m := range rows {
			out.Body.Items = append(out.Body.Items, mapMemo(m))
		}
		if hasMore && len(rows) > 0 {
			out.Body.NextCursor = encodeMemoCursor(rows[len(rows)-1].PublicID)
		}
		return out, nil
	}
}

func CreateMemo(deps Deps) func(context.Context, *CreateMemoInput) (*CreateMemoOutput, error) {
	return func(ctx context.Context, in *CreateMemoInput) (*CreateMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var created generated.CalendarMemo
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			if _, err := q.CreateMemo(ctx, generated.CreateMemoParams{
				PublicID:        pubID[:],
				WorkspaceID:     deps.WorkspaceID,
				CalendarID:      cal.ID,
				CreatedByUserID: userID,
				Title:           in.Body.Title,
				Body:            nullString(in.Body.Body),
				SortWeight:      in.Body.SortOrder,
			}); err != nil {
				return err
			}
			var err error
			// Read the stored row back so the response carries the database's
			// own timestamps rather than the server's clock.
			created, err = q.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
				PublicID:   pubID[:],
				CalendarID: cal.ID,
			})
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeMemoCreated,
				Summary:     in.Body.Title,
				Subject:     pubID[:],
				Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &CreateMemoOutput{Body: mapMemo(created)}, nil
	}
}

func UpdateMemo(deps Deps) func(context.Context, *UpdateMemoInput) (*UpdateMemoOutput, error) {
	return func(ctx context.Context, in *UpdateMemoInput) (*UpdateMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var updated generated.CalendarMemo
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			memo, err := lockMemo(ctx, q, cal.ID, in.MemoID)
			if err != nil {
				return err
			}

			// An omitted field keeps the stored value, so a partial update does
			// not blank out the rest of the memo. The stored value is the one
			// read under the lock, so a concurrent update of another field is
			// kept rather than written back over.
			title := memo.Title
			if in.Body.Title != nil {
				title = *in.Body.Title
			}
			body := memo.Body
			if in.Body.Body != nil {
				body = nullString(*in.Body.Body)
			}
			done := memo.Done
			if in.Body.Done != nil {
				done = *in.Body.Done
			}
			sortWeight := memo.SortWeight
			if in.Body.SortOrder != nil {
				sortWeight = *in.Body.SortOrder
			}

			if err := q.UpdateMemo(ctx, generated.UpdateMemoParams{
				Title:      title,
				Body:       body,
				Done:       done,
				SortWeight: sortWeight,
				ID:         memo.ID,
			}); err != nil {
				return err
			}
			updated, err = q.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
				PublicID:   memo.PublicID,
				CalendarID: cal.ID,
			})
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeMemoUpdated,
				Summary:     title,
				Subject:     memo.PublicID,
				Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &UpdateMemoOutput{Body: mapMemo(updated)}, nil
	}
}

func DeleteMemo(deps Deps) func(context.Context, *DeleteMemoInput) (*DeleteMemoOutput, error) {
	return func(ctx context.Context, in *DeleteMemoInput) (*DeleteMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			memo, err := lockMemo(ctx, q, cal.ID, in.MemoID)
			if err != nil {
				return err
			}
			// The log row is appended only for a delete this transaction made.
			res, err := q.SoftDeleteMemo(ctx, memo.ID)
			if err != nil {
				return err
			}
			if changed, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if !changed {
				return apierrors.MemoNotFound
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeMemoDeleted,
				Summary:     memo.Title,
				Subject:     memo.PublicID,
				Extra:       map[string]any{"calendarId": eventlog.UUID(cal.PublicID)},
			})
		})
		if err != nil {
			return nil, toAPIError(err)
		}

		return &DeleteMemoOutput{}, nil
	}
}
