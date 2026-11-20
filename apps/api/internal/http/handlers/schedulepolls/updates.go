package schedulepolls

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
)

func synchronizePollMembers(ctx context.Context, q *generated.Queries, pollID uint32, desired []uint32) (bool, error) {
	rows, err := q.ListSchedulePollMembers(ctx, pollID)
	if err != nil {
		return false, err
	}
	if len(rows) > maxPollMembers*2 {
		return false, apierrors.SchedulePollTooManyMembers
	}
	existing := make(map[uint32]generated.CalendarSchedulePollMember, len(rows))
	for _, row := range rows {
		existing[row.UserID] = row
	}
	wanted := make(map[uint32]struct{}, len(desired))
	for _, userID := range desired {
		wanted[userID] = struct{}{}
	}
	changed := false
	for _, row := range rows {
		_, keep := wanted[row.UserID]
		if keep || !row.Enabled {
			continue
		}
		if err := q.SetSchedulePollMemberEnabled(ctx, generated.SetSchedulePollMemberEnabledParams{
			Enabled: false,
			PollID:  pollID,
			UserID:  row.UserID,
		}); err != nil {
			return false, err
		}
		if err := q.DeleteSchedulePollMemberAnswers(ctx, generated.DeleteSchedulePollMemberAnswersParams{PollID: pollID, MemberID: row.ID}); err != nil {
			return false, err
		}
		changed = true
	}
	for _, userID := range desired {
		row, exists := existing[userID]
		if exists && row.Enabled {
			continue
		}
		if err := q.InsertSchedulePollMember(ctx, generated.InsertSchedulePollMemberParams{PollID: pollID, UserID: userID}); err != nil {
			return false, err
		}
		if exists {
			row, err = q.GetSchedulePollMemberByPollAndUser(ctx, generated.GetSchedulePollMemberByPollAndUserParams{PollID: pollID, UserID: userID})
			if err != nil {
				return false, err
			}
			if err := q.DeleteSchedulePollMemberAnswers(ctx, generated.DeleteSchedulePollMemberAnswersParams{PollID: pollID, MemberID: row.ID}); err != nil {
				return false, err
			}
		}
		changed = true
	}
	return changed, nil
}

func existingPollSlotsByPublicID(ctx context.Context, q *generated.Queries, pollID uint32) (map[string]generated.CalendarSchedulePollSlot, []generated.CalendarSchedulePollSlot, error) {
	rows, err := q.ListSchedulePollSlots(ctx, pollID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > maxPollStoredSlots {
		return nil, nil, apierrors.SchedulePollTooManySlots
	}
	byPublicID := make(map[string]generated.CalendarSchedulePollSlot, len(rows))
	for _, row := range rows {
		byPublicID[calresolve.PublicIDString(row.PublicID)] = row
	}
	return byPublicID, rows, nil
}

func parseOptionalSlotIDs(inputs []PollSlotInput) ([][]byte, error) {
	ids := make([][]byte, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for i, input := range inputs {
		if input.ID == "" {
			continue
		}
		id, err := parsePublicID(input.ID, apierrors.BadRequest)
		if err != nil {
			return nil, err
		}
		key := calresolve.PublicIDString(id)
		if _, duplicate := seen[key]; duplicate {
			return nil, apierrors.BadRequest
		}
		seen[key] = struct{}{}
		ids[i] = id
	}
	return ids, nil
}

func sameSlotTime(old generated.CalendarSchedulePollSlot, requested availability.Slot) bool {
	return old.StartAt.UTC().Equal(requested.Start.UTC()) && old.EndAt.UTC().Equal(requested.End.UTC())
}

// replacePollSlots keeps IDs and answers only for unchanged active candidates.
// A changed time, removed candidate, or candidate that was previously hard is
// archived; presenting it again creates a fresh ID and a fresh response task.
func replacePollSlots(ctx context.Context, q *generated.Queries, pollID uint32, inputs []PollSlotInput, slots []availability.Slot, states []availability.State) (changed, responseChanged bool, err error) {
	byPublicID, existing, err := existingPollSlotsByPublicID(ctx, q, pollID)
	if err != nil {
		return false, false, err
	}
	requestedIDs, err := parseOptionalSlotIDs(inputs)
	if err != nil {
		return false, false, err
	}
	newSlots := 0
	for i, slot := range slots {
		if len(requestedIDs[i]) == 0 {
			newSlots++
			continue
		}
		old, ok := byPublicID[calresolve.PublicIDString(requestedIDs[i])]
		if !ok {
			return false, false, apierrors.BadRequest
		}
		if !old.Enabled || !sameSlotTime(old, slot) {
			newSlots++
		}
	}
	if len(existing)+newSlots > maxPollStoredSlots {
		return false, false, apierrors.SchedulePollTooManySlots
	}
	usedExisting := make(map[uint32]struct{}, len(inputs))
	for i, slot := range slots {
		var old generated.CalendarSchedulePollSlot
		preserve := false
		if len(requestedIDs[i]) != 0 {
			old, preserve = byPublicID[calresolve.PublicIDString(requestedIDs[i])]
			if !preserve {
				return false, false, apierrors.BadRequest
			}
			// An inactive candidate is historical. Even if its clock time is the
			// same, it must be reissued with a new ID so old responses stay stale.
			preserve = old.Enabled && sameSlotTime(old, slot)
		}
		state := generated.CalendarSchedulePollSlotsAvailability(states[i])
		enabled := states[i] != availability.Hard
		if preserve {
			usedExisting[old.ID] = struct{}{}
			if old.Availability != state || old.Enabled != enabled {
				if err := q.UpdateSchedulePollSlotAvailability(ctx, generated.UpdateSchedulePollSlotAvailabilityParams{
					Availability: state,
					Enabled:      enabled,
					ID:           old.ID,
				}); err != nil {
					return false, false, err
				}
				changed = true
			}
			if old.Enabled != enabled {
				responseChanged = true
				if err := q.DeleteSchedulePollSlotAnswers(ctx, generated.DeleteSchedulePollSlotAnswersParams{PollID: pollID, SlotID: old.ID}); err != nil {
					return false, false, err
				}
			}
			continue
		}

		publicID, err := newPublicID()
		if err != nil {
			return false, false, err
		}
		if _, err := q.InsertSchedulePollSlot(ctx, generated.InsertSchedulePollSlotParams{
			PublicID:     publicID,
			PollID:       pollID,
			StartAt:      slot.Start.UTC(),
			EndAt:        slot.End.UTC(),
			Availability: state,
			Enabled:      enabled,
		}); err != nil {
			return false, false, err
		}
		changed = true
		responseChanged = true
	}
	for _, old := range existing {
		if !old.Enabled {
			continue
		}
		if _, keep := usedExisting[old.ID]; keep {
			continue
		}
		if err := q.DisableSchedulePollSlot(ctx, generated.DisableSchedulePollSlotParams{PollID: pollID, ID: old.ID}); err != nil {
			return false, false, err
		}
		if err := q.DeleteSchedulePollSlotAnswers(ctx, generated.DeleteSchedulePollSlotAnswersParams{PollID: pollID, SlotID: old.ID}); err != nil {
			return false, false, err
		}
		changed = true
		responseChanged = true
	}
	return changed, responseChanged, nil
}

func UpdatePoll(deps Deps) func(context.Context, *UpdatePollInput) (*UpdatePollOutput, error) {
	return func(ctx context.Context, in *UpdatePollInput) (*UpdatePollOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		newTitle := ""
		if in.Body.Title != nil {
			newTitle, err = validatePollTitle(*in.Body.Title)
			if err != nil {
				return nil, apiError(err)
			}
		}
		var requestedSlots []availability.Slot
		if in.Body.Slots != nil {
			requestedSlots, err = parsePollSlots(in.Body.Slots)
			if err != nil {
				return nil, apiError(err)
			}
		}

		cal, pollPublicID, err := authorizePollManager(ctx, deps, in.CalendarID, in.PollID, actor)
		if err != nil {
			return nil, apiError(err)
		}
		// A rejected memberIds is reported where it was before, after the poll checks.
		var requestedIDs []uint32
		var requestedErr error
		if in.Body.MemberIDs != nil {
			requestedIDs, requestedErr = memberIDsFromPublic(ctx, deps.Queries, deps.WorkspaceID, cal.ID, in.Body.MemberIDs)
		}

		var result PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			poll, err := lockPollForManager(ctx, q, deps, cal, pollPublicID)
			if err != nil {
				return err
			}
			roster, err := pollRoster(ctx, q, poll)
			if err != nil {
				return err
			}
			locked, err := lockPollMembers(ctx, q, cal.ID, actor, needManage, requestedIDs, roster)
			if err != nil {
				return err
			}
			actorMember := locked[actor]
			assigned := poll.ManagerUserID.Valid && uint32(poll.ManagerUserID.Int32) == actor
			if !assigned && !in.Body.TakeOver {
				return apierrors.SchedulePollAccessDenied
			}
			if !assigned && in.Body.TakeOver {
				organizerEligible, err := pollManagerEligible(ctx, q, poll)
				if err != nil {
					return err
				}
				if organizerEligible {
					return apierrors.SchedulePollAccessDenied
				}
			}
			if poll.Status != generated.CalendarSchedulePollsStatusOpen {
				return apierrors.SchedulePollConflict
			}
			if poll.Version != in.Body.Version {
				return apierrors.SchedulePollStale
			}
			managerChanged := !assigned && in.Body.TakeOver
			if newTitle == "" {
				newTitle = poll.Title
			}
			oldTitle := poll.Title
			if in.Body.Title == nil {
				newTitle = oldTitle
			}

			activeMembers, currentMemberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			memberIDs := currentMemberIDs
			memberChanged := false
			if in.Body.MemberIDs != nil {
				if requestedErr != nil {
					return requestedErr
				}
				memberIDs = requestedIDs
				if !slices.Equal(currentMemberIDs, memberIDs) {
					memberChanged, err = synchronizePollMembers(ctx, q, poll.ID, memberIDs)
					if err != nil {
						return err
					}
				}
			}
			_ = activeMembers

			candidateChanged := false
			candidateResponseChanged := false
			availabilityChanged := false
			if in.Body.Slots != nil {
				states, err := candidateAvailability(ctx, q, deps.WorkspaceID, cal.ID, memberIDs, requestedSlots)
				if err != nil {
					return err
				}
				if !slices.Contains(states, availability.Open) && !slices.Contains(states, availability.Soft) {
					return apierrors.SchedulePollSlotUnavailable
				}
				candidateChanged, candidateResponseChanged, err = replacePollSlots(ctx, q, poll.ID, in.Body.Slots, requestedSlots, states)
				if err != nil {
					return err
				}
			} else if memberChanged {
				availabilityChanged, candidateResponseChanged, err = refreshPollAvailability(ctx, q, deps.WorkspaceID, cal.ID, poll)
				if err != nil {
					return err
				}
			}

			responseChanged := memberChanged || candidateResponseChanged
			organizerOrRosterChanged := managerChanged || oldTitle != newTitle || memberChanged || candidateChanged
			if organizerOrRosterChanged {
				var responseRevision uint32
				if responseChanged {
					responseRevision = 1
				}
				if err := q.UpdateSchedulePollOrganizer(ctx, generated.UpdateSchedulePollOrganizerParams{
					Title:           newTitle,
					ManagerUserID:   sql.NullInt32{Int32: int32(actor), Valid: true},
					ResponseVersion: responseRevision,
					ID:              poll.ID,
				}); err != nil {
					return err
				}
				if responseChanged {
					if err := q.AdvanceSchedulePollAnswerVersions(ctx, poll.ID); err != nil {
						return err
					}
				}
			} else if candidateChanged || availabilityChanged {
				var responseRevision uint32
				if candidateResponseChanged {
					responseRevision = 1
				}
				if err := q.AdvanceSchedulePollVersion(ctx, generated.AdvanceSchedulePollVersionParams{
					ResponseVersion: responseRevision,
					ID:              poll.ID,
				}); err != nil {
					return err
				}
				if candidateResponseChanged {
					if err := q.AdvanceSchedulePollAnswerVersions(ctx, poll.ID); err != nil {
						return err
					}
				}
			}

			updated, err := q.GetSchedulePollByPublicID(ctx, generated.GetSchedulePollByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    poll.PublicID,
			})
			if err != nil {
				return err
			}
			result, err = detailResponse(ctx, q, avatars.New(q, deps.Storage), updated, cal.PublicID, actorMember.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		return &UpdatePollOutput{Body: result}, nil
	}
}

func parseFinalOwnerID(value *string) ([]byte, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	id, err := parsePublicID(*value, apierrors.BadRequest)
	if err != nil {
		return nil, false, err
	}
	return id, true, nil
}

func parseFinalSlotID(value string) ([]byte, error) {
	return parsePublicID(value, apierrors.BadRequest)
}

func samePublicID(a []byte, b sql.NullString) bool {
	return b.Valid && string(a) == b.String
}

func durationWithinLimit(start, end time.Time) bool {
	return end.After(start) && end.Sub(start) <= 24*time.Hour
}

var _ = errors.Is
