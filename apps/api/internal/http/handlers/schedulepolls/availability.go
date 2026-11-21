package schedulepolls

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
)

func parsePollSlots(inputs []PollSlotInput) ([]availability.Slot, error) {
	if len(inputs) == 0 {
		return nil, apierrors.BadRequest
	}
	if len(inputs) > maxPollSlots {
		return nil, apierrors.SchedulePollTooManySlots
	}
	slots := make([]availability.Slot, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	var earliest, latest time.Time
	for _, input := range inputs {
		start, err := time.Parse(time.RFC3339Nano, input.StartAt)
		if err != nil {
			return nil, apierrors.BadRequest
		}
		end, err := time.Parse(time.RFC3339Nano, input.EndAt)
		if err != nil || !end.After(start) || end.Sub(start) > 24*time.Hour {
			return nil, apierrors.BadRequest
		}
		start = start.UTC().Truncate(time.Millisecond)
		end = end.UTC().Truncate(time.Millisecond)
		if !end.After(start) {
			return nil, apierrors.BadRequest
		}
		key := start.Format(time.RFC3339Nano) + "/" + end.Format(time.RFC3339Nano)
		if _, duplicate := seen[key]; duplicate {
			return nil, apierrors.BadRequest
		}
		seen[key] = struct{}{}
		slots = append(slots, availability.Slot{Start: start, End: end})
		if earliest.IsZero() || start.Before(earliest) {
			earliest = start
		}
		if latest.IsZero() || end.After(latest) {
			latest = end
		}
	}
	if latest.Sub(earliest) > 366*24*time.Hour {
		return nil, apierrors.SchedulePollTooManySlots
	}
	return slots, nil
}

func toCandidateSlots(slots []availability.Slot, states []availability.State) []CandidateSlotResponse {
	result := make([]CandidateSlotResponse, 0, len(slots))
	for i, slot := range slots {
		if states[i] == availability.Hard {
			continue
		}
		result = append(result, CandidateSlotResponse{
			StartAt:      slot.Start.UTC(),
			EndAt:        slot.End.UTC(),
			Availability: string(states[i]),
			Active:       true,
		})
	}
	return result
}

func stateForPollSlot(slot generated.CalendarSchedulePollSlot) availability.Slot {
	return availability.Slot{Start: slot.StartAt.UTC(), End: slot.EndAt.UTC()}
}

// GetAvailabilitySharing reads the source-calendar consent setting.
func GetAvailabilitySharing(deps Deps) func(context.Context, *CalendarMembersInput) (*AvailabilitySharingOutput, error) {
	return func(ctx context.Context, in *CalendarMembersInput) (*AvailabilitySharingOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		cal, err := calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, in.CalendarID, actor)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		enabled, err := deps.Queries.GetScheduleAvailabilitySharing(ctx, cal.ID)
		if err != nil {
			return nil, apierrors.Internal(ctx, fmt.Errorf("get schedule availability sharing: %w", err))
		}
		out := &AvailabilitySharingOutput{}
		out.Body.Enabled = enabled
		return out, nil
	}
}

// SetAvailabilitySharing stores a manager-controlled source-calendar consent.
func SetAvailabilitySharing(deps Deps) func(context.Context, *AvailabilitySharingInput) (*AvailabilitySharingOutput, error) {
	return func(ctx context.Context, in *AvailabilitySharingInput) (*AvailabilitySharingOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		cal, err := calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, in.CalendarID, actor)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockManage(ctx, q, cal.ID, actor); err != nil {
				return err
			}
			currentCal, err := q.GetCalendarByID(ctx, cal.ID)
			if err != nil {
				return err
			}
			if in.Body.Enabled {
				if string(currentCal.Kind) != "personal" {
					return apierrors.BadRequest
				}
				rows, err := q.ListSchedulePollCalendarMembers(ctx, generated.ListSchedulePollCalendarMembersParams{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  currentCal.ID,
					Limit:       3,
				})
				if err != nil {
					return err
				}
				if len(rows) < 2 {
					return apierrors.BadRequest
				}
			}
			return q.SetScheduleAvailabilitySharing(ctx, generated.SetScheduleAvailabilitySharingParams{CalendarID: currentCal.ID, Enabled: in.Body.Enabled})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		out := &AvailabilitySharingOutput{}
		out.Body.Enabled = in.Body.Enabled
		return out, nil
	}
}

// SearchAvailability returns only open/soft times and never reveals the
// event or member that caused a fixed conflict.
func SearchAvailability(deps Deps) func(context.Context, *SearchAvailabilityInput) (*SearchAvailabilityOutput, error) {
	return func(ctx context.Context, in *SearchAvailabilityInput) (*SearchAvailabilityOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		var slots []availability.Slot
		if len(in.Body.Slots) > 0 {
			slots, err = parsePollSlots(in.Body.Slots)
		} else {
			slots, err = generateCandidateSlots(in.Body.Timezone, in.Body.FromDate, in.Body.ToDate, in.Body.StartTime, in.Body.EndTime, in.Body.DurationMinutes)
		}
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			if errors.Is(err, availability.ErrLimit) {
				return nil, apierrors.ToHuma(apierrors.SchedulePollTooManySlots)
			}
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if len(slots) == 0 {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if _, err := time.LoadLocation(in.Body.Timezone); err != nil || in.Body.Timezone == "Local" {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		slots = upcomingSlots(slots, time.Now())

		var states []availability.State
		err = withReadSnapshot(ctx, deps.DB, func(q *generated.Queries) error {
			cal, member, err := calresolve.Member(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			if !managerRole(member.Role) {
				return apierrors.CalendarRoleRequired
			}
			memberIDs, err := memberIDsFromPublic(ctx, q, deps.WorkspaceID, cal.ID, in.Body.MemberIDs)
			if err != nil || len(slots) == 0 {
				return err
			}
			states, err = availability.Check(ctx, q, deps.WorkspaceID, cal.ID, memberIDs, slots)
			return err
		})
		if err != nil {
			return nil, mapAvailabilityHandlerError(ctx, err)
		}
		return &SearchAvailabilityOutput{Body: toCandidateSlots(slots, states)}, nil
	}
}

func mapAvailabilityHandlerError(ctx context.Context, err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	if errors.Is(err, availability.ErrInvalid) || errors.Is(err, availability.ErrMembership) {
		return apierrors.ToHuma(apierrors.BadRequest)
	}
	if errors.Is(err, availability.ErrLimit) {
		return apierrors.ToHuma(apierrors.SchedulePollTooManySlots)
	}
	return apierrors.Internal(ctx, fmt.Errorf("check schedule poll availability: %w", err))
}

func candidateAvailability(ctx context.Context, q *generated.Queries, workspaceID, calendarID uint32, members []uint32, slots []availability.Slot) ([]availability.State, error) {
	states, err := availability.Check(ctx, q, workspaceID, calendarID, members, slots)
	if err == nil {
		return states, nil
	}
	switch {
	case errors.Is(err, availability.ErrInvalid):
		return nil, apierrors.BadRequest
	case errors.Is(err, availability.ErrLimit):
		return nil, apierrors.SchedulePollTooManySlots
	case errors.Is(err, availability.ErrMembership):
		return nil, apierrors.SchedulePollStale
	default:
		return nil, err
	}
}
