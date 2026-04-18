package memos

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

type Deps struct {
	Queries *generated.Queries
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func resolveCalendar(ctx context.Context, q *generated.Queries, calPubID string, userID uint32) (generated.Calendar, error) {
	pubBytes, err := parseUUID(calPubID)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	cal, err := q.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, apierrors.InternalUnexpected
	}
	_, err = q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID, UserID: userID,
	})
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarAccessDenied
	}
	return cal, nil
}

func mapMemo(m generated.Memo) MemoResponse {
	return MemoResponse{
		ID:        pubIDToHex(m.PublicID),
		Title:     m.Title,
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
		cal, err := resolveCalendar(ctx, deps.Queries, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		pubID, _ := uuid.NewV7()
		_, err = deps.Queries.CreateMemo(ctx, generated.CreateMemoParams{
			PublicID:    pubID[:],
			CalendarID:  cal.ID,
			Title:       in.Body.Title,
			SortOrder:   in.Body.SortOrder,
			CreatedBy:   userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &CreateMemoOutput{}
		out.Body = MemoResponse{
			ID:        pubID.String(),
			Title:     in.Body.Title,
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
		cal, err := resolveCalendar(ctx, deps.Queries, in.CalendarID, userID)
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

		return &UpdateMemoOutput{Body: mapMemo(updated)}, nil
	}
}

func DeleteMemo(deps Deps) func(context.Context, *DeleteMemoInput) (*DeleteMemoOutput, error) {
	return func(ctx context.Context, in *DeleteMemoInput) (*DeleteMemoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps.Queries, in.CalendarID, userID)
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

		return &DeleteMemoOutput{}, nil
	}
}
