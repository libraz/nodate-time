package events

import (
	"context"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// maxChecklistItemsListed caps one event's checklist. The list is read as a
// whole -- it is what somebody ticks off -- so splitting it across pages
// would be a worse answer than the ceiling; this only keeps a runaway writer
// from making one modal unbounded.
const maxChecklistItemsListed = 500

func mapChecklistItem(item generated.CalendarEventChecklistItem) ChecklistItemResponse {
	return ChecklistItemResponse{
		ID:        pubIDToHex(item.PublicID),
		Title:     item.Title,
		Done:      item.Done,
		SortOrder: int(item.SortWeight),
		CreatedAt: item.CreatedAt,
	}
}

// ListChecklistItems returns all checklist items for a given event.
func ListChecklistItems(deps Deps) func(context.Context, *ListChecklistInput) (*ListChecklistOutput, error) {
	return func(ctx context.Context, in *ListChecklistInput) (*ListChecklistOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListChecklistItems(ctx, generated.ListChecklistItemsParams{
			WorkspaceID: deps.WorkspaceID,
			EventID:     evt.ID,
			Limit:       maxChecklistItemsListed,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListChecklistOutput{Body: make([]ChecklistItemResponse, 0, len(rows))}
		for _, item := range rows {
			out.Body = append(out.Body, mapChecklistItem(item))
		}
		return out, nil
	}
}

// CreateChecklistItem adds a new checklist item to an event.
func CreateChecklistItem(deps Deps) func(context.Context, *CreateChecklistItemInput) (*CreateChecklistItemOutput, error) {
	return func(ctx context.Context, in *CreateChecklistItemInput) (*CreateChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := q.CreateChecklistItem(ctx, generated.CreateChecklistItemParams{
				PublicID:        pubID[:],
				WorkspaceID:     deps.WorkspaceID,
				EventID:         evt.ID,
				Title:           in.Body.Title,
				Done:            false,
				SortWeight:      int32(in.Body.SortOrder),
				CreatedByUserID: userID,
			}); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeChecklistAdded, in.Body.Title, pubID[:])
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		item, err := deps.Queries.GetChecklistItemByPublicID(ctx, pubID[:])
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &CreateChecklistItemOutput{}
		out.Body = mapChecklistItem(item)
		return out, nil
	}
}

// UpdateChecklistItem modifies an existing checklist item.
func UpdateChecklistItem(deps Deps) func(context.Context, *UpdateChecklistItemInput) (*UpdateChecklistItemOutput, error) {
	return func(ctx context.Context, in *UpdateChecklistItemInput) (*UpdateChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		itemPub, err := parseUUID(in.ItemID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}
		item, err := deps.Queries.GetChecklistItemByPublicID(ctx, itemPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}
		if item.EventID != evt.ID {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}

		// sortOrder is unchanged when omitted so title/done edits cannot
		// silently reorder the item to position zero.
		sortOrder := item.SortWeight
		if in.Body.SortOrder != nil {
			sortOrder = int32(*in.Body.SortOrder)
		}
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdateChecklistItem(ctx, generated.UpdateChecklistItemParams{
				Title:      in.Body.Title,
				Done:       in.Body.Done,
				SortWeight: sortOrder,
				ID:         item.ID,
			}); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeChecklistSet, in.Body.Title, item.PublicID)
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		updated, err := deps.Queries.GetChecklistItemByPublicID(ctx, item.PublicID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &UpdateChecklistItemOutput{}
		out.Body = mapChecklistItem(updated)
		return out, nil
	}
}

// DeleteChecklistItem removes a checklist item from an event.
func DeleteChecklistItem(deps Deps) func(context.Context, *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
	return func(ctx context.Context, in *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, err := resolveEventForEdit(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		itemPub, err := parseUUID(in.ItemID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}
		item, err := deps.Queries.GetChecklistItemByPublicID(ctx, itemPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}
		if item.EventID != evt.ID {
			return nil, apierrors.ToHuma(apierrors.ChecklistItemNotFound)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.SoftDeleteChecklistItem(ctx, item.ID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeChecklistGone, item.Title, item.PublicID)
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteChecklistItemOutput{}, nil
	}
}
