package schedulepolls

import (
	"context"
	"database/sql"
	"errors"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
)

func CancelPoll(deps Deps) func(context.Context, *CancelPollInput) (*PollMutationOutput, error) {
	return func(ctx context.Context, in *CancelPollInput) (*PollMutationOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		var result PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			cal, member, poll, err := lockPollForManager(ctx, q, deps, in.CalendarID, in.PollID, actor)
			if err != nil {
				return err
			}
			if !poll.ManagerUserID.Valid || uint32(poll.ManagerUserID.Int32) != actor {
				return apierrors.SchedulePollAccessDenied
			}
			if poll.Status == generated.CalendarSchedulePollsStatusFinalized {
				return apierrors.SchedulePollConflict
			}
			if poll.Status == generated.CalendarSchedulePollsStatusOpen {
				if in.Body.Version != poll.Version {
					return apierrors.SchedulePollStale
				}
				if err := q.SetSchedulePollCancelled(ctx, poll.ID); err != nil {
					return err
				}
				poll.Status = generated.CalendarSchedulePollsStatusCancelled
				poll.Version++
			}
			result, err = detailResponse(ctx, q, poll, cal.PublicID, member.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		return &PollMutationOutput{Body: result}, nil
	}
}

func SchedulePollActions(deps Deps) func(context.Context, *PollActionsInput) (*PollActionsOutput, error) {
	return func(ctx context.Context, _ *PollActionsInput) (*PollActionsOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		out := &PollActionsOutput{}
		out.Body.Actions = make([]PollAction, 0)
		err = withReadSnapshot(ctx, deps.DB, func(q *generated.Queries) error {
			rows, err := q.ListSchedulePollActionPolls(ctx, generated.ListSchedulePollActionPollsParams{
				UserID:        actor,
				WorkspaceID:   deps.WorkspaceID,
				ManagerUserID: sql.NullInt32{Int32: int32(actor), Valid: true},
				UserID_2:      actor,
				Limit:         maxPollActions + 1,
			})
			if err != nil {
				return err
			}
			if len(rows) > maxPollActions {
				return apierrors.SchedulePollTooMuchHistory
			}
			managerRoles := make(map[uint32]bool)
			for _, row := range rows {
				action := PollAction{
					CalendarID:   calresolve.PublicIDString(row.CalendarPublicID),
					CalendarName: row.CalendarName,
					PollID:       calresolve.PublicIDString(row.PublicID),
					Title:        row.Title,
				}
				target, eligible, err := currentPollMember(ctx, q, row.ID, actor)
				if err != nil {
					return err
				}
				if eligible {
					pending, err := q.CountSchedulePollMemberPendingSlots(ctx, generated.CountSchedulePollMemberPendingSlotsParams{
						PollID: row.ID, MemberID: target.ID,
					})
					if err != nil {
						return err
					}
					if pending > 0 {
						action.Kind = "answer"
						out.Body.Actions = append(out.Body.Actions, action)
					}
				}
				if !row.ManagerUserID.Valid || uint32(row.ManagerUserID.Int32) != actor {
					continue
				}
				isManager, checked := managerRoles[row.CalendarID]
				if !checked {
					member, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
						CalendarID: row.CalendarID, UserID: actor,
					})
					if err != nil && !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					isManager = err == nil && calresolve.CanManage(member.Role)
					managerRoles[row.CalendarID] = isManager
				}
				if isManager {
					kind, err := organizerSchedulePollAction(ctx, q, row.ID)
					if err != nil {
						return err
					}
					if kind != "" {
						action.Kind = kind
						out.Body.Actions = append(out.Body.Actions, action)
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, apiError(err)
		}
		return out, nil
	}
}

func organizerSchedulePollAction(ctx context.Context, q *generated.Queries, pollID uint32) (string, error) {
	slots, err := q.ListSchedulePollSlots(ctx, pollID)
	if err != nil {
		return "", err
	}
	selectable := make(map[string]struct{})
	for _, slot := range slots {
		if slot.Enabled {
			selectable[string(slot.PublicID)] = struct{}{}
		}
	}
	if len(selectable) == 0 {
		return "add-slots", nil
	}
	members, err := q.CountSchedulePollCurrentMembers(ctx, pollID)
	if err != nil || members == 0 {
		return "", err
	}
	responded, err := q.CountSchedulePollCurrentResponded(ctx, pollID)
	if err != nil || responded != members {
		return "", err
	}
	answers, err := q.ListSchedulePollAnswersForManager(ctx, pollID)
	if err != nil {
		return "", err
	}
	for _, answer := range answers {
		if answer.Value == generated.CalendarSchedulePollAnswersValueNo {
			delete(selectable, string(answer.SlotPublicID))
		}
	}
	if len(selectable) == 0 {
		return "add-slots", nil
	}
	return "finalize", nil
}
