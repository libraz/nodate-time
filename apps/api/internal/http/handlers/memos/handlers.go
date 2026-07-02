package memos

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/audit"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

type Deps struct {
	Queries *generated.Queries
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

func resolveCalendar(ctx context.Context, q *generated.Queries, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, q, calPubID, userID)
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, q *generated.Queries, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Write(ctx, q, calPubID, userID)
}

func resolveCalendarMember(ctx context.Context, q *generated.Queries, calPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	return calresolve.Member(ctx, q, calPubID, userID)
}

func mapMemo(m generated.Memo) MemoResponse {
	return MemoResponse{
		ID:        pubIDToHex(m.PublicID),
		Title:     m.Title,
		Body:      m.Body,
		Done:      m.Done,
		SortOrder: m.SortOrder,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func ListMemos(deps Deps) func(context.Context, *ListMemosInput) (*ListMemosOutput, error) {
	return func(ctx context.Context, in *ListMemosInput) (*ListMemosOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps.Queries, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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
		cal, err := resolveCalendarWrite(ctx, deps.Queries, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		pubID, _ := uuid.NewV7()
		result, err := deps.Queries.CreateMemo(ctx, generated.CreateMemoParams{
			PublicID:   pubID[:],
			CalendarID: cal.ID,
			Title:      in.Body.Title,
			Body:       in.Body.Body,
			SortOrder:  in.Body.SortOrder,
			CreatedBy:  userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		memoID64, _ := result.LastInsertId()
		audit.Record(ctx, deps.Queries, cal.ID, uint32(memoID64), pubID[:], audit.EntityMemo, audit.ActionCreate, userID, in.Body.Title)

		out := &CreateMemoOutput{}
		out.Body = MemoResponse{
			ID:        pubID.String(),
			Title:     in.Body.Title,
			Body:      in.Body.Body,
			SortOrder: in.Body.SortOrder,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		return out, nil
	}
}

func UpdateMemo(deps Deps) func(context.Context, *UpdateMemoInput) (*UpdateMemoOutput, error) {
	return func(ctx context.Context, in *UpdateMemoInput) (*UpdateMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps.Queries, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		memoPub, err := parseUUID(in.MemoID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemoNotFound)
		}

		memo, err := deps.Queries.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
			PublicID:   memoPub,
			CalendarID: cal.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.MemoNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		err = deps.Queries.UpdateMemo(ctx, generated.UpdateMemoParams{
			Title:     in.Body.Title,
			Body:      in.Body.Body,
			Done:      in.Body.Done,
			SortOrder: in.Body.SortOrder,
			ID:        memo.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		updated, err := deps.Queries.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
			PublicID:   memoPub,
			CalendarID: cal.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		audit.Record(ctx, deps.Queries, cal.ID, memo.ID, memo.PublicID, audit.EntityMemo, audit.ActionUpdate, userID, in.Body.Title)

		return &UpdateMemoOutput{Body: mapMemo(updated)}, nil
	}
}

func DeleteMemo(deps Deps) func(context.Context, *DeleteMemoInput) (*DeleteMemoOutput, error) {
	return func(ctx context.Context, in *DeleteMemoInput) (*DeleteMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps.Queries, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		memoPub, err := parseUUID(in.MemoID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemoNotFound)
		}

		memo, err := deps.Queries.GetMemoByPublicID(ctx, generated.GetMemoByPublicIDParams{
			PublicID:   memoPub,
			CalendarID: cal.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.MemoNotFound)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		err = deps.Queries.DeleteMemo(ctx, memo.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		audit.Record(ctx, deps.Queries, cal.ID, memo.ID, memo.PublicID, audit.EntityMemo, audit.ActionDelete, userID, memo.Title)

		return &DeleteMemoOutput{}, nil
	}
}
