package events

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
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
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		var item generated.CalendarEventChecklistItem
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			if _, err := q.CreateChecklistItem(ctx, generated.CreateChecklistItemParams{
				PublicID:        pubID[:],
				WorkspaceID:     deps.WorkspaceID,
				EventID:         w.evt.ID,
				Title:           in.Body.Title,
				Done:            false,
				SortWeight:      int32(in.Body.SortOrder),
				CreatedByUserID: userID,
			}); err != nil {
				return err
			}
			if err := logEventActivity(ctx, q, deps, cal.ID, userID, w.evt,
				eventlog.TypeChecklistCreated, "itemId", in.Body.Title, pubID[:]); err != nil {
				return err
			}
			item, err = q.GetChecklistItemByPublicID(ctx, pubID[:])
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &CreateChecklistItemOutput{Body: mapChecklistItem(item)}, nil
	}
}

// lockChecklistItem reads one live item of the event under its row lock.
func lockChecklistItem(ctx context.Context, q *generated.Queries, eventID uint32, itemID string) (generated.CalendarEventChecklistItem, error) {
	pub, err := parseUUID(itemID)
	if err != nil {
		return generated.CalendarEventChecklistItem{}, apierrors.ChecklistItemNotFound
	}
	item, err := q.GetChecklistItemForUpdate(ctx, generated.GetChecklistItemForUpdateParams{
		PublicID: pub,
		EventID:  eventID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarEventChecklistItem{}, apierrors.ChecklistItemNotFound
	}
	return item, err
}

// UpdateChecklistItem modifies an existing checklist item.
func UpdateChecklistItem(deps Deps) func(context.Context, *UpdateChecklistItemInput) (*UpdateChecklistItemOutput, error) {
	return func(ctx context.Context, in *UpdateChecklistItemInput) (*UpdateChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var updated generated.CalendarEventChecklistItem
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			item, err := lockChecklistItem(ctx, q, w.evt.ID, in.ItemID)
			if err != nil {
				return err
			}
			// Every field keeps its stored value when omitted, merged over the
			// row as it stands under the lock rather than as it was first read.
			title := item.Title
			if in.Body.Title != nil {
				title = *in.Body.Title
			}
			done := item.Done
			if in.Body.Done != nil {
				done = *in.Body.Done
			}
			sortOrder := item.SortWeight
			if in.Body.SortOrder != nil {
				sortOrder = int32(*in.Body.SortOrder)
			}
			if err := q.UpdateChecklistItem(ctx, generated.UpdateChecklistItemParams{
				Title:      title,
				Done:       done,
				SortWeight: sortOrder,
				ID:         item.ID,
			}); err != nil {
				return err
			}
			if err := logEventActivity(ctx, q, deps, cal.ID, userID, w.evt,
				eventlog.TypeChecklistUpdated, "itemId", title, item.PublicID); err != nil {
				return err
			}
			updated, err = q.GetChecklistItemByPublicID(ctx, item.PublicID)
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &UpdateChecklistItemOutput{Body: mapChecklistItem(updated)}, nil
	}
}

// DeleteChecklistItem removes a checklist item from an event. Only the delete
// that retired the row logs: a second one racing it waits on the item's lock
// and then finds nothing live to remove.
func DeleteChecklistItem(deps Deps) func(context.Context, *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
	return func(ctx context.Context, in *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			item, err := lockChecklistItem(ctx, q, w.evt.ID, in.ItemID)
			if err != nil {
				return err
			}
			res, err := q.SoftDeleteChecklistItem(ctx, item.ID)
			if err != nil {
				return err
			}
			removed, err := blobref.ChangedOne(res)
			if err != nil {
				return err
			}
			if !removed {
				return apierrors.ChecklistItemNotFound
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, w.evt,
				eventlog.TypeChecklistDeleted, "itemId", item.Title, item.PublicID)
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &DeleteChecklistItemOutput{}, nil
	}
}
