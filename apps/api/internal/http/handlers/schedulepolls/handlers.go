package schedulepolls

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/availability"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

const (
	maxPollMembers     = 500
	maxPollSlots       = 256
	maxPollStoredSlots = 4096
	maxPollActions     = 1000
	maxPollHistory     = 500
	finalizeTries      = 3
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	WorkspaceID uint32
}

func actorID(ctx context.Context) (uint32, error) {
	id, ok := middleware.ActorFromContext(ctx)
	if !ok || id == 0 {
		return 0, apierrors.ToHuma(apierrors.AuthTokenInvalid)
	}
	return id, nil
}

func apiError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
}

func internalError(ctx context.Context, operation string, err error) error {
	slog.ErrorContext(ctx, operation, "error", err)
	return apierrors.ToHuma(apierrors.InternalUnexpected)
}

func parsePublicID(value string, invalid *apierrors.Spec) ([]byte, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, invalid
	}
	return id[:], nil
}

func newPublicID() ([]byte, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return id[:], nil
}

func pollByPublicID(ctx context.Context, q *generated.Queries, workspaceID, calendarID uint32, publicID []byte) (generated.CalendarSchedulePoll, error) {
	poll, err := q.GetSchedulePollByPublicID(ctx, generated.GetSchedulePollByPublicIDParams{
		WorkspaceID: workspaceID,
		CalendarID:  calendarID,
		PublicID:    publicID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarSchedulePoll{}, apierrors.SchedulePollNotFound
	}
	return poll, err
}

func listCalendarMembers(ctx context.Context, q *generated.Queries, workspaceID, calendarID uint32, limit int32) ([]generated.ListSchedulePollCalendarMembersRow, error) {
	rows, err := q.ListSchedulePollCalendarMembers(ctx, generated.ListSchedulePollCalendarMembersParams{
		WorkspaceID: workspaceID,
		CalendarID:  calendarID,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) > maxPollMembers {
		return nil, apierrors.SchedulePollTooManyMembers
	}
	return rows, nil
}

func memberIDsFromPublic(ctx context.Context, q *generated.Queries, workspaceID, calendarID uint32, publicIDs []string) ([]uint32, error) {
	if len(publicIDs) == 0 || len(publicIDs) > maxPollMembers {
		if len(publicIDs) > maxPollMembers {
			return nil, apierrors.SchedulePollTooManyMembers
		}
		return nil, apierrors.BadRequest
	}
	rows, err := listCalendarMembers(ctx, q, workspaceID, calendarID, maxPollMembers+1)
	if err != nil {
		return nil, err
	}
	byPublicID := make(map[string]uint32, len(rows))
	for _, row := range rows {
		byPublicID[calresolve.PublicIDString(row.PublicID)] = row.UserID
	}
	ids := make([]uint32, 0, len(publicIDs))
	seen := make(map[uint32]struct{}, len(publicIDs))
	for _, value := range publicIDs {
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, apierrors.BadRequest
		}
		id, ok := byPublicID[parsed.String()]
		if !ok {
			return nil, apierrors.BadRequest
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, apierrors.BadRequest
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

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

func availabilityError(err error) error {
	switch {
	case errors.Is(err, availability.ErrInvalid):
		return apierrors.ToHuma(apierrors.BadRequest)
	case errors.Is(err, availability.ErrLimit):
		return apierrors.ToHuma(apierrors.SchedulePollTooManySlots)
	case errors.Is(err, availability.ErrMembership):
		return apierrors.ToHuma(apierrors.SchedulePollStale)
	default:
		return apierrors.ToHuma(apierrors.InternalUnexpected)
	}
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

func managerRole(role generated.CalendarMembersRole) bool {
	return calresolve.CanManage(role)
}

func pollManagerEligible(ctx context.Context, q *generated.Queries, poll generated.CalendarSchedulePoll) (bool, error) {
	return q.IsSchedulePollManagerEligible(ctx, poll.ID)
}

func currentPollMember(ctx context.Context, q *generated.Queries, pollID, userID uint32) (generated.CalendarSchedulePollMember, bool, error) {
	member, err := q.GetSchedulePollMemberByPollAndUser(ctx, generated.GetSchedulePollMemberByPollAndUserParams{PollID: pollID, UserID: userID})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !member.Enabled) {
		return generated.CalendarSchedulePollMember{}, false, nil
	}
	return member, err == nil, err
}

func detailResponse(ctx context.Context, q *generated.Queries, poll generated.CalendarSchedulePoll, calendarPublicID []byte, role generated.CalendarMembersRole, actor uint32) (PollDetailResponse, error) {
	memberRows, err := q.ListSchedulePollMembersCurrent(ctx, poll.ID)
	if err != nil {
		return PollDetailResponse{}, err
	}
	memberByUser := make(map[uint32]generated.ListSchedulePollMembersCurrentRow, len(memberRows))
	memberByPublicID := make(map[string]uint32, len(memberRows))
	for _, member := range memberRows {
		memberByUser[member.UserID] = member
		memberByPublicID[calresolve.PublicIDString(member.PublicID)] = member.UserID
	}
	callerMember, isTarget := memberByUser[actor]

	slotRows, err := q.ListSchedulePollSlots(ctx, poll.ID)
	if err != nil {
		return PollDetailResponse{}, err
	}
	slots := make([]PollSlotResponse, 0, len(slotRows))
	activeSlotIDs := make(map[string]struct{}, len(slotRows))
	for _, slot := range slotRows {
		publicID := calresolve.PublicIDString(slot.PublicID)
		slots = append(slots, PollSlotResponse{
			ID:           publicID,
			StartAt:      slot.StartAt.UTC(),
			EndAt:        slot.EndAt.UTC(),
			Availability: string(slot.Availability),
			Active:       slot.Enabled,
		})
		if slot.Enabled {
			activeSlotIDs[publicID] = struct{}{}
		}
	}

	memberCount, err := q.CountSchedulePollCurrentMembers(ctx, poll.ID)
	if err != nil {
		return PollDetailResponse{}, err
	}
	respondedCount, err := q.CountSchedulePollCurrentResponded(ctx, poll.ID)
	if err != nil {
		return PollDetailResponse{}, err
	}

	canManage := managerRole(role) && poll.ManagerUserID.Valid && uint32(poll.ManagerUserID.Int32) == actor
	managerEligible, err := pollManagerEligible(ctx, q, poll)
	if err != nil {
		return PollDetailResponse{}, err
	}
	canTakeOver := managerRole(role) && !canManage && !managerEligible && poll.Status == generated.CalendarSchedulePollsStatusOpen
	if !canManage && !canTakeOver && !isTarget {
		return PollDetailResponse{}, apierrors.SchedulePollNotFound
	}
	canRespond := isTarget && poll.Status == generated.CalendarSchedulePollsStatusOpen && len(activeSlotIDs) > 0

	response := PollDetailResponse{
		ID:              calresolve.PublicIDString(poll.PublicID),
		CalendarID:      calresolve.PublicIDString(calendarPublicID),
		Title:           poll.Title,
		Timezone:        poll.Timezone,
		Status:          string(poll.Status),
		Version:         poll.Version,
		ResponseVersion: poll.ResponseVersion,
		ReviewVersion:   poll.ResponseVersion,
		CanManage:       canManage,
		CanTakeOver:     canTakeOver,
		CanRespond:      canRespond,
		Slots:           slots,
		Members:         []PollMemberResponse{},
		MyAnswers:       []PollAnswerResponse{},
		RespondedCount:  respondedCount,
		MemberCount:     memberCount,
	}
	if poll.ConfirmedEventPublicID.Valid {
		response.ConfirmedEventID = calresolve.PublicIDString([]byte(poll.ConfirmedEventPublicID.String))
	}
	if poll.SelectedSlotPublicID.Valid {
		response.SelectedSlotID = calresolve.PublicIDString([]byte(poll.SelectedSlotPublicID.String))
	}

	if canManage {
		for _, member := range memberRows {
			response.Members = append(response.Members, PollMemberResponse{ID: calresolve.PublicIDString(member.PublicID), Name: member.DisplayName})
		}
		responseRows, err := q.ListSchedulePollAnswersForManager(ctx, poll.ID)
		if err != nil {
			return PollDetailResponse{}, err
		}
		answersByMember := make(map[string][]PollAnswerResponse, len(memberRows))
		for _, member := range memberRows {
			id := calresolve.PublicIDString(member.PublicID)
			answersByMember[id] = []PollAnswerResponse{}
		}
		for _, answer := range responseRows {
			id := calresolve.PublicIDString(answer.MemberPublicID)
			answersByMember[id] = append(answersByMember[id], PollAnswerResponse{
				SlotID: calresolve.PublicIDString(answer.SlotPublicID),
				Value:  string(answer.Value),
			})
		}
		for _, member := range memberRows {
			id := calresolve.PublicIDString(member.PublicID)
			response.Responses = append(response.Responses, PollMemberAnswersResponse{MemberID: id, Answers: answersByMember[id]})
		}
	}
	if isTarget {
		pollMember, _, err := currentPollMember(ctx, q, poll.ID, actor)
		if err != nil {
			return PollDetailResponse{}, err
		}
		response.ResponseVersion = pollMember.AnswerVersion
		answers, err := q.ListSchedulePollAnswersForUser(ctx, generated.ListSchedulePollAnswersForUserParams{PollID: poll.ID, UserID: callerMember.UserID})
		if err != nil {
			return PollDetailResponse{}, err
		}
		for _, answer := range answers {
			response.MyAnswers = append(response.MyAnswers, PollAnswerResponse{
				SlotID: calresolve.PublicIDString(answer.SlotPublicID),
				Value:  string(answer.Value),
			})
		}
	}
	return response, nil
}

func loadAuthorizedPoll(ctx context.Context, deps Deps, q *generated.Queries, calendarID, pollID string, actor uint32) (generated.Calendar, generated.CalendarMember, generated.CalendarSchedulePoll, error) {
	cal, member, err := calresolve.Member(ctx, q, deps.WorkspaceID, calendarID, actor)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, err
	}
	pollPublicID, err := parsePublicID(pollID, apierrors.SchedulePollNotFound)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, err
	}
	poll, err := pollByPublicID(ctx, q, deps.WorkspaceID, cal.ID, pollPublicID)
	return cal, member, poll, err
}

// ListCalendarMembers returns the complete eligible roster or an explicit
// error if a calendar exceeds the poll's supported size.
func ListCalendarMembers(deps Deps) func(context.Context, *CalendarMembersInput) (*CalendarMembersOutput, error) {
	return func(ctx context.Context, in *CalendarMembersInput) (*CalendarMembersOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		out := &CalendarMembersOutput{}
		err = withReadSnapshot(ctx, deps.DB, func(q *generated.Queries) error {
			cal, err := calresolve.Manage(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			rows, err := listCalendarMembers(ctx, q, deps.WorkspaceID, cal.ID, maxPollMembers+1)
			if err != nil {
				return err
			}
			out.Body.Members = make([]PollMemberResponse, 0, len(rows))
			for _, row := range rows {
				out.Body.Members = append(out.Body.Members, PollMemberResponse{ID: calresolve.PublicIDString(row.PublicID), Name: row.DisplayName})
			}
			return nil
		})
		if err != nil {
			return nil, apiError(err)
		}
		return out, nil
	}
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
			return nil, apiError(err)
		}
		enabled, err := deps.Queries.GetScheduleAvailabilitySharing(ctx, cal.ID)
		if err != nil {
			return nil, internalError(ctx, "get schedule availability sharing", err)
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
			return nil, apiError(err)
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
			return nil, apiError(err)
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
			if err != nil {
				return err
			}
			states, err = availability.Check(ctx, q, deps.WorkspaceID, cal.ID, memberIDs, slots)
			return err
		})
		if err != nil {
			return nil, mapAvailabilityHandlerError(ctx, err)
		}
		out := &SearchAvailabilityOutput{}
		out.Body.Slots = toCandidateSlots(slots, states)
		return out, nil
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
	return internalError(ctx, "check schedule poll availability", err)
}

func withReadSnapshot(ctx context.Context, db *sql.DB, fn func(*generated.Queries) error) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(generated.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func withSerializable(ctx context.Context, db *sql.DB, fn func(*generated.Queries) error) error {
	var lastErr error
	for attempt := 0; attempt < finalizeTries; attempt++ {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return err
		}
		err = fn(generated.New(tx))
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryableTransactionError(err) || attempt+1 == finalizeTries {
			return err
		}
	}
	return lastErr
}

func retryableTransactionError(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == 1205 || mysqlErr.Number == 1213)
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

func validatePollTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 500 {
		return "", apierrors.BadRequest
	}
	return title, nil
}

func validateTimezone(timezone string) error {
	if timezone == "" || timezone == "Local" {
		return apierrors.BadRequest
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return apierrors.BadRequest
	}
	return nil
}

func lockPollForManager(ctx context.Context, q *generated.Queries, deps Deps, calendarID, pollID string, actor uint32) (generated.Calendar, generated.CalendarMember, generated.CalendarSchedulePoll, error) {
	cal, err := calresolve.Manage(ctx, q, deps.WorkspaceID, calendarID, actor)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, err
	}
	member, err := calresolve.LockManage(ctx, q, cal.ID, actor)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, err
	}
	publicID, err := parsePublicID(pollID, apierrors.SchedulePollNotFound)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, err
	}
	poll, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  cal.ID,
		PublicID:    publicID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.Calendar{}, generated.CalendarMember{}, generated.CalendarSchedulePoll{}, apierrors.SchedulePollNotFound
	}
	return cal, member, poll, err
}

func lockRequestedMembers(ctx context.Context, q *generated.Queries, calendarID uint32, memberIDs []uint32) error {
	locked, err := calresolve.LockMembers(ctx, q, calendarID, memberIDs...)
	if err != nil {
		return err
	}
	for _, id := range memberIDs {
		if _, ok := locked[id]; !ok {
			return apierrors.SchedulePollStale
		}
	}
	return nil
}

// CreatePoll snapshots the selected recipients and rechecks candidate times
// under the same serializable transaction that publishes the request.
func CreatePoll(deps Deps) func(context.Context, *CreatePollInput) (*CreatePollOutput, error) {
	return func(ctx context.Context, in *CreatePollInput) (*CreatePollOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		title, err := validatePollTitle(in.Body.Title)
		if err != nil {
			return nil, apiError(err)
		}
		if err := validateTimezone(in.Body.Timezone); err != nil {
			return nil, apiError(err)
		}
		slots, err := parsePollSlots(in.Body.Slots)
		if err != nil {
			return nil, apiError(err)
		}
		pollPublicID, err := newPublicID()
		if err != nil {
			return nil, internalError(ctx, "generate schedule poll id", err)
		}
		slotPublicIDs := make([][]byte, len(slots))
		for i := range slotPublicIDs {
			slotPublicIDs[i], err = newPublicID()
			if err != nil {
				return nil, internalError(ctx, "generate schedule poll candidate id", err)
			}
		}
		var detail PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			cal, err := calresolve.Read(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			actorMember, err := calresolve.LockManage(ctx, q, cal.ID, actor)
			if err != nil {
				return err
			}
			memberIDs, err := memberIDsFromPublic(ctx, q, deps.WorkspaceID, cal.ID, in.Body.MemberIDs)
			if err != nil {
				return err
			}
			if err := lockRequestedMembers(ctx, q, cal.ID, memberIDs); err != nil {
				return err
			}
			states, err := candidateAvailability(ctx, q, deps.WorkspaceID, cal.ID, memberIDs, slots)
			if err != nil {
				return err
			}
			if !slices.Contains(states, availability.Open) && !slices.Contains(states, availability.Soft) {
				return apierrors.SchedulePollSlotUnavailable
			}
			result, err := q.CreateSchedulePoll(ctx, generated.CreateSchedulePollParams{
				PublicID:        pollPublicID,
				WorkspaceID:     deps.WorkspaceID,
				CalendarID:      cal.ID,
				CreatedByUserID: sql.NullInt32{Int32: int32(actor), Valid: true},
				ManagerUserID:   sql.NullInt32{Int32: int32(actor), Valid: true},
				Title:           title,
				Timezone:        in.Body.Timezone,
			})
			if err != nil {
				return err
			}
			pollID64, err := result.LastInsertId()
			if err != nil {
				return err
			}
			pollID := uint32(pollID64)
			for _, memberID := range memberIDs {
				if err := q.InsertSchedulePollMember(ctx, generated.InsertSchedulePollMemberParams{PollID: pollID, UserID: memberID}); err != nil {
					return err
				}
			}
			for i, slot := range slots {
				if _, err := q.InsertSchedulePollSlot(ctx, generated.InsertSchedulePollSlotParams{
					PublicID:     slotPublicIDs[i],
					PollID:       pollID,
					StartAt:      slot.Start.UTC(),
					EndAt:        slot.End.UTC(),
					Availability: generated.CalendarSchedulePollSlotsAvailability(states[i]),
					Enabled:      states[i] != availability.Hard,
				}); err != nil {
					return err
				}
			}
			poll, err := q.GetSchedulePollByPublicID(ctx, generated.GetSchedulePollByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    pollPublicID,
			})
			if err != nil {
				return err
			}
			detail, err = detailResponse(ctx, q, poll, cal.PublicID, actorMember.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		return &CreatePollOutput{Body: detail}, nil
	}
}

// GetPoll loads a private poll only for a current recipient, its assigned
// manager, or a calendar manager taking over an abandoned poll.
func GetPoll(deps Deps) func(context.Context, *GetPollInput) (*GetPollOutput, error) {
	return func(ctx context.Context, in *GetPollInput) (*GetPollOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		var detail PollDetailResponse
		err = withReadSnapshot(ctx, deps.DB, func(q *generated.Queries) error {
			cal, member, poll, err := loadAuthorizedPoll(ctx, deps, q, in.CalendarID, in.PollID, actor)
			if err != nil {
				return err
			}
			detail, err = detailResponse(ctx, q, poll, cal.PublicID, member.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		return &GetPollOutput{Body: detail}, nil
	}
}

// ListPolls returns the calendar's history filtered to current recipients and
// managers. It refuses an oversized history response instead of truncating.
func ListPolls(deps Deps) func(context.Context, *ListPollsInput) (*ListPollsOutput, error) {
	return func(ctx context.Context, in *ListPollsInput) (*ListPollsOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		out := &ListPollsOutput{}
		err = withReadSnapshot(ctx, deps.DB, func(q *generated.Queries) error {
			cal, member, err := calresolve.Member(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			polls, err := q.ListSchedulePollsVisibleByCalendar(ctx, generated.ListSchedulePollsVisibleByCalendarParams{
				WorkspaceID:   deps.WorkspaceID,
				CalendarID:    cal.ID,
				UserID:        actor,
				UserID_2:      actor,
				ManagerUserID: sql.NullInt32{Int32: int32(actor), Valid: true},
				Limit:         maxPollHistory + 1,
			})
			if err != nil {
				return err
			}
			if len(polls) > maxPollHistory {
				return apierrors.SchedulePollTooMuchHistory
			}
			out.Body.Polls = make([]PollDetailResponse, 0, len(polls))
			for _, poll := range polls {
				detail, err := detailResponse(ctx, q, poll, cal.PublicID, member.Role, actor)
				if err != nil {
					return err
				}
				out.Body.Polls = append(out.Body.Polls, detail)
			}
			return nil
		})
		if err != nil {
			return nil, apiError(err)
		}
		return out, nil
	}
}

func activePollMembers(ctx context.Context, q *generated.Queries, pollID uint32) ([]generated.ListSchedulePollMembersCurrentRow, []uint32, error) {
	rows, err := q.ListSchedulePollMembersCurrent(ctx, pollID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) > maxPollMembers {
		return nil, nil, apierrors.SchedulePollTooManyMembers
	}
	ids := make([]uint32, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	slices.Sort(ids)
	return rows, ids, nil
}

// refreshPollAvailability updates each candidate's aggregate state. If a
// candidate leaves or re-enters the active set, its old answers are cleared
// and response_version advances so nobody's prior completion is reused.
func refreshPollAvailability(ctx context.Context, q *generated.Queries, workspaceID uint32, calendarID uint32, poll generated.CalendarSchedulePoll) (changed, responseChanged bool, err error) {
	_, memberIDs, err := activePollMembers(ctx, q, poll.ID)
	if err != nil {
		return false, false, err
	}
	slotRows, err := q.ListSchedulePollSlots(ctx, poll.ID)
	if err != nil {
		return false, false, err
	}
	if len(slotRows) > maxPollStoredSlots {
		return false, false, apierrors.SchedulePollTooManySlots
	}
	activeSlots := make([]generated.CalendarSchedulePollSlot, 0, len(slotRows))
	for _, slot := range slotRows {
		if slot.Enabled {
			activeSlots = append(activeSlots, slot)
		}
	}
	if len(activeSlots) > maxPollSlots {
		return false, false, apierrors.SchedulePollTooManySlots
	}
	if len(activeSlots) == 0 {
		return false, false, nil
	}
	slots := make([]availability.Slot, len(activeSlots))
	for i, slot := range activeSlots {
		slots[i] = stateForPollSlot(slot)
	}
	states, err := candidateAvailability(ctx, q, workspaceID, calendarID, memberIDs, slots)
	if err != nil {
		return false, false, err
	}
	for i, slot := range activeSlots {
		enabled := states[i] != availability.Hard
		state := generated.CalendarSchedulePollSlotsAvailability(states[i])
		if slot.Availability == state && slot.Enabled == enabled {
			continue
		}
		if err := q.UpdateSchedulePollSlotAvailability(ctx, generated.UpdateSchedulePollSlotAvailabilityParams{
			Availability: state,
			Enabled:      enabled,
			ID:           slot.ID,
		}); err != nil {
			return false, false, err
		}
		changed = true
		if slot.Enabled != enabled {
			responseChanged = true
			if err := q.DeleteSchedulePollSlotAnswers(ctx, generated.DeleteSchedulePollSlotAnswersParams{PollID: poll.ID, SlotID: slot.ID}); err != nil {
				return false, false, err
			}
		}
	}
	if changed {
		var responseRevision uint32
		if responseChanged {
			responseRevision = 1
			if err := q.AdvanceSchedulePollAnswerVersions(ctx, poll.ID); err != nil {
				return false, false, err
			}
		}
		if err := q.AdvanceSchedulePollVersion(ctx, generated.AdvanceSchedulePollVersionParams{ResponseVersion: responseRevision, ID: poll.ID}); err != nil {
			return false, false, err
		}
	}
	return changed, responseChanged, nil
}

func RefreshAvailability(deps Deps) func(context.Context, *PollMutationInput) (*PollMutationOutput, error) {
	return func(ctx context.Context, in *PollMutationInput) (*PollMutationOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		var result PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			cal, err := calresolve.Read(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			member, err := calresolve.LockMember(ctx, q, cal.ID, actor)
			if err != nil {
				return err
			}
			pollPublicID, err := parsePublicID(in.PollID, apierrors.SchedulePollNotFound)
			if err != nil {
				return err
			}
			poll, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    pollPublicID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return apierrors.SchedulePollNotFound
			}
			if err != nil {
				return err
			}
			current, memberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			isTarget := false
			for _, target := range current {
				if target.UserID == actor {
					isTarget = true
					break
				}
			}
			isOrganizer := managerRole(member.Role) && poll.ManagerUserID.Valid && uint32(poll.ManagerUserID.Int32) == actor
			if !isTarget && !isOrganizer {
				return apierrors.SchedulePollNotFound
			}
			if poll.Status != generated.CalendarSchedulePollsStatusOpen {
				return apierrors.SchedulePollConflict
			}
			lockedMembers, err := calresolve.LockMembers(ctx, q, cal.ID, memberIDs...)
			if err != nil {
				return err
			}
			if len(lockedMembers) != len(memberIDs) {
				return apierrors.SchedulePollStale
			}
			if _, _, err := refreshPollAvailability(ctx, q, deps.WorkspaceID, cal.ID, poll); err != nil {
				return err
			}
			updated, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    pollPublicID,
			})
			if err != nil {
				return err
			}
			result, err = detailResponse(ctx, q, updated, cal.PublicID, member.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		return &PollMutationOutput{Body: result}, nil
	}
}

func SetResponse(deps Deps) func(context.Context, *SetResponseInput) (*PollMutationOutput, error) {
	return func(ctx context.Context, in *SetResponseInput) (*PollMutationOutput, error) {
		actor, err := actorID(ctx)
		if err != nil {
			return nil, err
		}
		var result PollDetailResponse
		staleAfterRefresh := false
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			staleAfterRefresh = false
			cal, err := calresolve.Read(ctx, q, deps.WorkspaceID, in.CalendarID, actor)
			if err != nil {
				return err
			}
			member, err := calresolve.LockMember(ctx, q, cal.ID, actor)
			if err != nil {
				return err
			}
			pollPublicID, err := parsePublicID(in.PollID, apierrors.SchedulePollNotFound)
			if err != nil {
				return err
			}
			poll, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    pollPublicID,
			})
			if errors.Is(err, sql.ErrNoRows) {
				return apierrors.SchedulePollNotFound
			}
			if err != nil {
				return err
			}
			_, memberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			if !slices.Contains(memberIDs, actor) {
				return apierrors.SchedulePollNotFound
			}
			if poll.Status != generated.CalendarSchedulePollsStatusOpen {
				return apierrors.SchedulePollConflict
			}
			lockedMembers, err := calresolve.LockMembers(ctx, q, cal.ID, memberIDs...)
			if err != nil {
				return err
			}
			if len(lockedMembers) != len(memberIDs) {
				return apierrors.SchedulePollStale
			}
			changed, _, err := refreshPollAvailability(ctx, q, deps.WorkspaceID, cal.ID, poll)
			if err != nil {
				return err
			}
			if changed {
				staleAfterRefresh = true
				updated, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  cal.ID,
					PublicID:    pollPublicID,
				})
				if err != nil {
					return err
				}
				result, err = detailResponse(ctx, q, updated, cal.PublicID, member.Role, actor)
				return err
			}
			pollMember, eligible, err := currentPollMember(ctx, q, poll.ID, actor)
			if err != nil {
				return err
			}
			if !eligible {
				return apierrors.SchedulePollNotFound
			}
			if poll.Version != in.Body.Version || pollMember.AnswerVersion != in.Body.ResponseVersion {
				return apierrors.SchedulePollStale
			}
			slots, err := q.ListSchedulePollSlots(ctx, poll.ID)
			if err != nil {
				return err
			}
			activeSlots := make(map[string]generated.CalendarSchedulePollSlot, len(slots))
			for _, slot := range slots {
				if slot.Enabled {
					activeSlots[calresolve.PublicIDString(slot.PublicID)] = slot
				}
			}
			existingAnswers, err := q.ListSchedulePollAnswersForUser(ctx, generated.ListSchedulePollAnswersForUserParams{
				PollID: poll.ID,
				UserID: actor,
			})
			if err != nil {
				return err
			}
			answersBySlot := make(map[string]generated.CalendarSchedulePollAnswersValue, len(existingAnswers))
			for _, answer := range existingAnswers {
				answersBySlot[calresolve.PublicIDString(answer.SlotPublicID)] = answer.Value
			}
			seen := make(map[uint32]struct{}, len(in.Body.Answers))
			answerChanged := false
			for _, answer := range in.Body.Answers {
				publicID, err := parsePublicID(answer.SlotID, apierrors.BadRequest)
				if err != nil {
					return err
				}
				slot, ok := activeSlots[calresolve.PublicIDString(publicID)]
				if !ok {
					return apierrors.SchedulePollStale
				}
				if _, duplicate := seen[slot.ID]; duplicate {
					return apierrors.BadRequest
				}
				seen[slot.ID] = struct{}{}
				if answer.Value != "yes" && answer.Value != "maybe" && answer.Value != "no" {
					return apierrors.BadRequest
				}
				value := generated.CalendarSchedulePollAnswersValue(answer.Value)
				if oldValue, exists := answersBySlot[calresolve.PublicIDString(publicID)]; exists && oldValue == value {
					continue
				}
				if err := q.UpsertSchedulePollAnswer(ctx, generated.UpsertSchedulePollAnswerParams{
					PollID:   poll.ID,
					SlotID:   slot.ID,
					MemberID: pollMember.ID,
					UserID:   actor,
					Value:    value,
				}); err != nil {
					return err
				}
				answerChanged = true
			}
			if answerChanged {
				if err := q.AdvanceSchedulePollMemberAnswerVersion(ctx, generated.AdvanceSchedulePollMemberAnswerVersionParams{
					PollID: poll.ID, ID: pollMember.ID,
				}); err != nil {
					return err
				}
				if err := q.AdvanceSchedulePollReviewVersion(ctx, poll.ID); err != nil {
					return err
				}
			}
			updated, err := q.GetSchedulePollByPublicID(ctx, generated.GetSchedulePollByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				PublicID:    pollPublicID,
			})
			if err != nil {
				return err
			}
			result, err = detailResponse(ctx, q, updated, cal.PublicID, member.Role, actor)
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		if staleAfterRefresh {
			return nil, apiError(apierrors.SchedulePollStale)
		}
		return &PollMutationOutput{Body: result}, nil
	}
}
