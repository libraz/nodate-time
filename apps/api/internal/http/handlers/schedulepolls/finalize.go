package schedulepolls

import (
	"context"
	"database/sql"
	"errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
)

func FinalizePoll(deps Deps) func(context.Context, *FinalizePollInput) (*PollMutationOutput, error) {
	return func(ctx context.Context, in *FinalizePollInput) (*PollMutationOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		slotPublicID, err := parseFinalSlotID(in.Body.SlotID)
		if err != nil {
			return nil, apiError(err)
		}
		ownerPublicID, hasOwner, err := parseFinalOwnerID(in.Body.OwnerID)
		if err != nil {
			return nil, apiError(err)
		}
		eventPublicID, err := newPublicID()
		if err != nil {
			return nil, internalError(ctx, "generate confirmed event id", err)
		}

		cal, pollPublicID, err := authorizePollManager(ctx, deps, in.CalendarID, in.PollID, actor)
		if err != nil {
			return nil, apiError(err)
		}
		var result PollDetailResponse
		blockedByNewFixedEvent := false
		availabilityChangedAtFinalize := false
		err = dbtx.RunSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			poll, err := lockPollForManager(ctx, q, deps, cal, pollPublicID)
			if err != nil {
				return err
			}
			blockedByNewFixedEvent = false
			availabilityChangedAtFinalize = false
			roster, err := pollRoster(ctx, q, poll)
			if err != nil {
				return err
			}
			locked, err := lockPollMembers(ctx, q, cal.ID, actor, needManage, nil, roster)
			if err != nil {
				return err
			}
			member := locked[actor]
			if !poll.ManagerUserID.Valid || uint32(poll.ManagerUserID.Int32) != actor {
				return apierrors.SchedulePollAccessDenied
			}
			if poll.Status == generated.CalendarSchedulePollsStatusFinalized {
				if !samePublicID(slotPublicID, poll.SelectedSlotPublicID) || !poll.ConfirmedEventPublicID.Valid {
					return apierrors.SchedulePollConflict
				}
				result, err = detailResponse(ctx, q, avatars.New(q, deps.Storage), poll, cal.PublicID, member.Role, actor)
				return err
			}
			if poll.Status != generated.CalendarSchedulePollsStatusOpen {
				return apierrors.SchedulePollConflict
			}
			if err := checkIfMatch(in.IfMatch, pollETag(poll.Version, poll.ResponseVersion), apierrors.SchedulePollStale); err != nil {
				return err
			}

			members, memberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			if len(memberIDs) == 0 {
				return apierrors.SchedulePollConflict
			}
			// Recipients who have not answered do not hold the poll open; the
			// client confirms closing without them.

			slot, err := q.GetSchedulePollSlotByPublicID(ctx, generated.GetSchedulePollSlotByPublicIDParams{
				PollID:   poll.ID,
				PublicID: slotPublicID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return apierrors.SchedulePollConflict
			}
			if err != nil {
				return err
			}
			if !slot.Enabled || slot.Availability == generated.CalendarSchedulePollSlotsAvailabilityHard {
				return apierrors.SchedulePollSlotUnavailable
			}
			states, err := candidateAvailability(ctx, q, deps.WorkspaceID, cal.ID, memberIDs, []availability.Slot{stateForPollSlot(slot)})
			if err != nil {
				return err
			}
			if states[0] == availability.Hard {
				if err := q.UpdateSchedulePollSlotAvailability(ctx, generated.UpdateSchedulePollSlotAvailabilityParams{
					Availability: generated.CalendarSchedulePollSlotsAvailabilityHard,
					Enabled:      false,
					ID:           slot.ID,
				}); err != nil {
					return err
				}
				if err := q.DeleteSchedulePollSlotAnswers(ctx, generated.DeleteSchedulePollSlotAnswersParams{PollID: poll.ID, SlotID: slot.ID}); err != nil {
					return err
				}
				if err := q.AdvanceSchedulePollVersion(ctx, generated.AdvanceSchedulePollVersionParams{ResponseVersion: 1, ID: poll.ID}); err != nil {
					return err
				}
				if err := q.AdvanceSchedulePollAnswerVersions(ctx, poll.ID); err != nil {
					return err
				}
				blockedByNewFixedEvent = true
				return nil
			}
			if slot.Availability != generated.CalendarSchedulePollSlotsAvailability(states[0]) {
				if err := q.UpdateSchedulePollSlotAvailability(ctx, generated.UpdateSchedulePollSlotAvailabilityParams{
					Availability: generated.CalendarSchedulePollSlotsAvailability(states[0]),
					Enabled:      true,
					ID:           slot.ID,
				}); err != nil {
					return err
				}
				if err := q.AdvanceSchedulePollVersion(ctx, generated.AdvanceSchedulePollVersionParams{ResponseVersion: 0, ID: poll.ID}); err != nil {
					return err
				}
				availabilityChangedAtFinalize = true
				return nil
			}

			answerRows, err := q.ListSchedulePollAnswersForManager(ctx, poll.ID)
			if err != nil {
				return err
			}
			maybeSelected := false
			for _, answer := range answerRows {
				if string(answer.SlotPublicID) == string(slot.PublicID) && answer.Value == generated.CalendarSchedulePollAnswersValueNo {
					return apierrors.SchedulePollConflict
				}
				if string(answer.SlotPublicID) == string(slot.PublicID) && answer.Value == generated.CalendarSchedulePollAnswersValueMaybe {
					maybeSelected = true
				}
			}
			if (states[0] == availability.Soft || maybeSelected) && !in.Body.Acknowledged {
				return apierrors.SchedulePollConfirmationRequired
			}

			memberByPublicID := make(map[string]uint32, len(members))
			managerIsRecipient := false
			for _, recipient := range members {
				memberByPublicID[calresolve.PublicIDString(recipient.PublicID)] = recipient.UserID
				if recipient.UserID == actor {
					managerIsRecipient = true
				}
			}
			ownerUserID := uint32(0)
			if hasOwner {
				var ok bool
				ownerUserID, ok = memberByPublicID[calresolve.PublicIDString(ownerPublicID)]
				if !ok {
					return apierrors.BadRequest
				}
			} else if managerIsRecipient {
				ownerUserID = actor
			} else {
				return apierrors.BadRequest
			}

			if !durationWithinLimit(slot.StartAt, slot.EndAt) {
				return apierrors.BadRequest
			}
			attendeeIDs := make([]uint32, 0, len(members))
			for _, recipient := range members {
				attendeeIDs = append(attendeeIDs, recipient.UserID)
			}
			created, err := eventstore.Insert(ctx, q, generated.CreateCalendarEventParams{
				PublicID:           eventPublicID,
				WorkspaceID:        deps.WorkspaceID,
				CalendarID:         cal.ID,
				Kind:               generated.CalendarEventsKindEvent,
				Visibility:         generated.CalendarEventsVisibilityConfidential,
				ShowAs:             generated.CalendarEventsShowAsBusy,
				Flexibility:        generated.CalendarEventsFlexibilityFixed,
				Title:              poll.Title,
				AllDay:             false,
				StartAt:            sql.NullTime{Time: slot.StartAt.UTC(), Valid: true},
				EndAt:              sql.NullTime{Time: slot.EndAt.UTC(), Valid: true},
				Timezone:           poll.Timezone,
				Location:           sql.NullString{},
				Memo:               sql.NullString{},
				URL:                sql.NullString{},
				OwnerUserID:        ownerUserID,
				CreatedByUserID:    actor,
				NotificationOffset: sql.NullInt32{},
				RecurrenceRule:     nil,
				RecurrenceEnd:      sql.NullTime{},
				BlockLabel:         sql.NullString{},
				SourceUid:          sql.NullString{},
			}, attendeeIDs, nil)
			if err != nil {
				return err
			}
			if err := q.SetSchedulePollFinalized(ctx, generated.SetSchedulePollFinalizedParams{
				SelectedSlotPublicID:   sql.NullString{String: string(slot.PublicID), Valid: true},
				ConfirmedEventPublicID: sql.NullString{String: string(created.PublicID), Valid: true},
				ID:                     poll.ID,
			}); err != nil {
				return err
			}
			updated, err := q.GetSchedulePollByPublicID(ctx, generated.GetSchedulePollByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    poll.PublicID,
			})
			if err != nil {
				return err
			}
			result, err = detailResponse(ctx, q, avatars.New(q, deps.Storage), updated, cal.PublicID, member.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		if blockedByNewFixedEvent {
			return nil, apiError(apierrors.SchedulePollSlotUnavailable)
		}
		if availabilityChangedAtFinalize {
			return nil, apiError(apierrors.SchedulePollStale)
		}
		return &PollMutationOutput{Body: result}, nil
	}
}
