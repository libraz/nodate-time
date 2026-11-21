package events

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
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

// maxChecklistItems is the most live items one event's checklist holds. The
// list is read as a whole -- it is what somebody ticks off -- so it is bounded
// when an item is added rather than paged.
const maxChecklistItems = 500

// rowETag names one revision of a row from its last write, in the same form
// as an event's tag.
func rowETag(updated sql.NullTime, created time.Time) string {
	stamp := created
	if updated.Valid {
		stamp = updated.Time
	}
	return `"` + stamp.UTC().Format("20060102T150405.000") + `"`
}

// checkIfMatch compares an If-Match with the current tag of a row, answering
// with the resource's own stale error when it names another revision.
func checkIfMatch(h precondition.IfMatch, current string, stale *apierrors.Spec) error {
	err := h.Check(current)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, precondition.ErrStale):
		return stale
	}
	return apierrors.BadIfMatch
}

func checklistETag(item generated.CalendarEventChecklistItem) string {
	return rowETag(item.UpdatedAt, item.CreatedAt)
}

func mapChecklistItem(item generated.CalendarEventChecklistItem) ChecklistItemResponse {
	return ChecklistItemResponse{
		ID:        pubIDToHex(item.PublicID),
		Title:     item.Title,
		Done:      item.Done,
		SortOrder: int(item.SortWeight),
		ETag:      checklistETag(item),
		CreatedAt: item.CreatedAt,
	}
}

// ListChecklistItems returns all checklist items for a given event.
func ListChecklistItems(deps Deps) func(context.Context, *ListChecklistInput) (*ListChecklistOutput, error) {
	return func(ctx context.Context, in *ListChecklistInput) (*ListChecklistOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, evt, err := readableSubresourceEvent(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListChecklistItems(ctx, generated.ListChecklistItemsParams{
			WorkspaceID: deps.WorkspaceID,
			EventID:     evt.ID,
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
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		var item generated.CalendarEventChecklistItem
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockDetailEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			count, err := q.CountChecklistItems(ctx, generated.CountChecklistItemsParams{
				WorkspaceID: deps.WorkspaceID,
				EventID:     w.evt.ID,
			})
			if err != nil {
				return err
			}
			if count >= maxChecklistItems {
				return apierrors.ChecklistLimitReached
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

		resp := mapChecklistItem(item)
		return &CreateChecklistItemOutput{ETag: resp.ETag, Body: resp}, nil
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
		// No member is nullable: there is no empty title, state or position
		// to clear it to.
		title, hasTitle := in.Body.Title.Get()
		if in.Body.Title.IsNull() || in.Body.Done.IsNull() || in.Body.SortOrder.IsNull() {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var updated generated.CalendarEventChecklistItem
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockDetailEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			item, err := lockChecklistItem(ctx, q, w.evt.ID, in.ItemID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, checklistETag(item), apierrors.ChecklistStale); err != nil {
				return err
			}
			// Every field keeps its stored value when omitted, merged over the
			// row as it stands under the lock rather than as it was first read.
			if !hasTitle {
				title = item.Title
			}
			done := item.Done
			if v, ok := in.Body.Done.Get(); ok {
				done = v
			}
			sortOrder := item.SortWeight
			if v, ok := in.Body.SortOrder.Get(); ok {
				sortOrder = int32(v)
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

		resp := mapChecklistItem(updated)
		return &UpdateChecklistItemOutput{ETag: resp.ETag, Body: resp}, nil
	}
}

// DeleteChecklistItem removes a checklist item from an event. Only the delete
// that retired the row logs: a second one racing it waits on the item's lock
// and then finds nothing live to remove.
func DeleteChecklistItem(deps Deps) func(context.Context, *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
	return func(ctx context.Context, in *DeleteChecklistItemInput) (*DeleteChecklistItemOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := subresourceWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockDetailEdit(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			item, err := lockChecklistItem(ctx, q, w.evt.ID, in.ItemID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, checklistETag(item), apierrors.ChecklistStale); err != nil {
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
