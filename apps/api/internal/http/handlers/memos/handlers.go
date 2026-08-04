package memos

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
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

// loadMemo resolves a memo public id inside the calendar the caller has
// already proved access to, so an id from another calendar cannot be
// reached by guessing.
func loadMemo(ctx context.Context, deps Deps, calID uint32, memoID string) (generated.CalendarMemo, error) {
	memoPub, err := parseUUID(memoID)
	if err != nil {
		return generated.CalendarMemo{}, apierrors.MemoNotFound
	}
	memo, err := deps.Queries.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
		PublicID:   memoPub,
		CalendarID: calID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.CalendarMemo{}, apierrors.MemoNotFound
		}
		return generated.CalendarMemo{}, apierrors.InternalUnexpected
	}
	return memo, nil
}

func ListMemos(deps Deps) func(context.Context, *ListMemosInput) (*ListMemosOutput, error) {
	return func(ctx context.Context, in *ListMemosInput) (*ListMemosOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListMemosByCalendar(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListMemosOutput{Body: make([]MemoResponse, 0, len(rows))}
		for _, m := range rows {
			out.Body = append(out.Body, mapMemo(m))
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
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

		memo, err := loadMemo(ctx, deps, cal.ID, in.MemoID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var updated generated.CalendarMemo
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdateMemo(ctx, generated.UpdateMemoParams{
				Title:      in.Body.Title,
				Body:       nullString(in.Body.Body),
				Done:       in.Body.Done,
				SortWeight: in.Body.SortOrder,
				ID:         memo.ID,
			}); err != nil {
				return err
			}
			var err error
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
				Summary:     in.Body.Title,
				Subject:     memo.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

		memo, err := loadMemo(ctx, deps, cal.ID, in.MemoID)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.SoftDeleteMemo(ctx, memo.ID); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeMemoDeleted,
				Summary:     memo.Title,
				Subject:     memo.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteMemoOutput{}, nil
	}
}
