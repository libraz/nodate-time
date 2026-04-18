package events

import (
	"context"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

func resolveEvent(ctx context.Context, deps Deps, calID uint32, eventID string) (generated.Event, error) {
	eid := eventID
	if parentUUID, _ := parseCompositeID(eid); parentUUID != "" {
		eid = parentUUID
	}
	evtPub, err := parseUUID(eid)
	if err != nil {
		return generated.Event{}, apierrors.EventNotFound
	}
	evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
	if err != nil || evt.CalendarID != calID {
		return generated.Event{}, apierrors.EventNotFound
	}
	return evt, nil
}

func mapChecklistItem(item generated.EventChecklistItem) ChecklistItemResponse {
	return ChecklistItemResponse{
		ID:        pubIDToHex(item.PublicID),
		Title:     item.Title,
		Done:      item.Done,
		SortOrder: int(item.SortOrder),
		CreatedAt: item.CreatedAt,
	}
}

// ListChecklistItems returns all checklist items for a given event.
func ListChecklistItems(deps Deps) func(context.Context, *ListChecklistInput) (*ListChecklistOutput, error) {
	return func(ctx context.Context, in *ListChecklistInput) (*ListChecklistOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		rows, err := deps.Queries.ListChecklistItems(ctx, evt.ID)
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		pubID, _ := uuid.NewV7()
		_, err = deps.Queries.CreateChecklistItem(ctx, generated.CreateChecklistItemParams{
			PublicID:  pubID[:],
			EventID:   evt.ID,
			Title:     in.Body.Title,
			Done:      false,
			SortOrder: int32(in.Body.SortOrder),
			CreatedBy: userID,
		})
		if err != nil {
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

		err = deps.Queries.UpdateChecklistItem(ctx, generated.UpdateChecklistItemParams{
			Title:     in.Body.Title,
			Done:      in.Body.Done,
			SortOrder: int32(in.Body.SortOrder),
			ID:        item.ID,
		})
		if err != nil {
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

		err = deps.Queries.DeleteChecklistItem(ctx, item.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteChecklistItemOutput{}, nil
	}
}
