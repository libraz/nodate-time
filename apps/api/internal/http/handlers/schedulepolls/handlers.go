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

// Every poll write locks in one order: calendar shared, poll row, then member
// rows ascending with the caller among them. The grant is read before the
// transaction, where a plain read takes no lock, and re-checked under lock by
// lockPollMembers; under SERIALIZABLE a read inside would S-lock the caller's
// row out of that order.

// authorizePollManager resolves the calendar and the caller's manager grant
// before the transaction.
func authorizePollManager(ctx context.Context, deps Deps, calendarID, pollID string, actor uint32) (generated.Calendar, []byte, error) {
	cal, err := calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, calendarID, actor)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	publicID, err := parsePublicID(pollID, apierrors.SchedulePollNotFound)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	return cal, publicID, nil
}

// lockPollForManager takes the calendar's shared lock and then the poll row.
func lockPollForManager(ctx context.Context, q *generated.Queries, deps Deps, cal generated.Calendar, publicID []byte) (generated.CalendarSchedulePoll, error) {
	if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
		return generated.CalendarSchedulePoll{}, err
	}
	poll, err := q.GetSchedulePollForUpdateByPublicID(ctx, generated.GetSchedulePollForUpdateByPublicIDParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  cal.ID,
		PublicID:    publicID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarSchedulePoll{}, apierrors.SchedulePollNotFound
	}
	return poll, err
}

// pollRoster lists the poll's enabled recipients and its organizer from the
// poll's own tables, so no calendar_members row is read before lockPollMembers.
func pollRoster(ctx context.Context, q *generated.Queries, poll generated.CalendarSchedulePoll) ([]uint32, error) {
	rows, err := q.ListSchedulePollMembers(ctx, poll.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]uint32, 0, len(rows)+1)
	for _, row := range rows {
		if row.Enabled {
			ids = append(ids, row.UserID)
		}
	}
	if poll.ManagerUserID.Valid {
		ids = append(ids, uint32(poll.ManagerUserID.Int32))
	}
	return ids, nil
}

// roleNeed is the calendar role a poll write requires of its caller.
type roleNeed int

const (
	needMember roleNeed = iota
	needManage
)

// lockPollMembers locks the caller, ids and roster in one ascending pass. The
// caller is checked first: CalendarAccessDenied once the membership is gone,
// CalendarRoleRequired when need is needManage and the role is below manager.
// Then every id in ids must still be live, or SchedulePollStale. A roster
// member who left the calendar is only absent from the result.
func lockPollMembers(ctx context.Context, q *generated.Queries, calendarID, caller uint32, need roleNeed, ids, roster []uint32) (map[uint32]generated.CalendarMember, error) {
	all := make([]uint32, 0, len(ids)+len(roster)+1)
	all = append(all, caller)
	all = append(all, ids...)
	all = append(all, roster...)
	locked, err := calresolve.LockMembers(ctx, q, calendarID, all...)
	if err != nil {
		return nil, err
	}
	member, ok := locked[caller]
	if !ok {
		return nil, apierrors.CalendarAccessDenied
	}
	if need == needManage && !calresolve.CanManage(member.Role) {
		return nil, apierrors.CalendarRoleRequired
	}
	for _, id := range ids {
		if _, ok := locked[id]; !ok {
			return nil, apierrors.SchedulePollStale
		}
	}
	return locked, nil
}

// checkPollParticipant admits a current recipient, or with allowOrganizer the
// assigned organizer holding a manager role, to an open poll.
func checkPollParticipant(poll generated.CalendarSchedulePoll, memberIDs []uint32, role generated.CalendarMembersRole, actor uint32, allowOrganizer bool) error {
	isTarget := slices.Contains(memberIDs, actor)
	isOrganizer := allowOrganizer && managerRole(role) && poll.ManagerUserID.Valid && uint32(poll.ManagerUserID.Int32) == actor
	if !isTarget && !isOrganizer {
		return apierrors.SchedulePollNotFound
	}
	if poll.Status != generated.CalendarSchedulePollsStatusOpen {
		return apierrors.SchedulePollConflict
	}
	return nil
}

// authorizePollParticipant runs checkPollParticipant on unlocked reads before
// the transaction, so a non-member or a closed poll is refused as before
// without taking a member lock. The run under lock is the authoritative one.
func authorizePollParticipant(ctx context.Context, deps Deps, calendarID, pollID string, actor uint32, allowOrganizer bool) (generated.Calendar, []byte, error) {
	cal, member, err := calresolve.Member(ctx, deps.Queries, deps.WorkspaceID, calendarID, actor)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	pollPublicID, err := parsePublicID(pollID, apierrors.SchedulePollNotFound)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	poll, err := pollByPublicID(ctx, deps.Queries, deps.WorkspaceID, cal.ID, pollPublicID)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	_, memberIDs, err := activePollMembers(ctx, deps.Queries, poll.ID)
	if err != nil {
		return generated.Calendar{}, nil, err
	}
	if err := checkPollParticipant(poll, memberIDs, member.Role, actor, allowOrganizer); err != nil {
		return generated.Calendar{}, nil, err
	}
	return cal, pollPublicID, nil
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
		cal, err := calresolve.Manage(ctx, deps.Queries, deps.WorkspaceID, in.CalendarID, actor)
		if err != nil {
			return nil, apiError(err)
		}
		memberIDs, err := memberIDsFromPublic(ctx, deps.Queries, deps.WorkspaceID, cal.ID, in.Body.MemberIDs)
		if err != nil {
			return nil, apiError(err)
		}
		var detail PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
				return err
			}
			locked, err := lockPollMembers(ctx, q, cal.ID, actor, needManage, memberIDs, nil)
			if err != nil {
				return err
			}
			actorMember := locked[actor]
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
		cal, pollPublicID, err := authorizePollParticipant(ctx, deps, in.CalendarID, in.PollID, actor, true)
		if err != nil {
			return nil, apiError(err)
		}
		var result PollDetailResponse
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
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
			roster, err := pollRoster(ctx, q, poll)
			if err != nil {
				return err
			}
			locked, err := lockPollMembers(ctx, q, cal.ID, actor, needMember, nil, roster)
			if err != nil {
				return err
			}
			member := locked[actor]
			_, memberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			if err := checkPollParticipant(poll, memberIDs, member.Role, actor, true); err != nil {
				return err
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
		cal, pollPublicID, err := authorizePollParticipant(ctx, deps, in.CalendarID, in.PollID, actor, false)
		if err != nil {
			return nil, apiError(err)
		}
		var result PollDetailResponse
		staleAfterRefresh := false
		err = withSerializable(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
				return err
			}
			staleAfterRefresh = false
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
			roster, err := pollRoster(ctx, q, poll)
			if err != nil {
				return err
			}
			locked, err := lockPollMembers(ctx, q, cal.ID, actor, needMember, nil, roster)
			if err != nil {
				return err
			}
			member := locked[actor]
			_, memberIDs, err := activePollMembers(ctx, q, poll.ID)
			if err != nil {
				return err
			}
			if err := checkPollParticipant(poll, memberIDs, member.Role, actor, false); err != nil {
				return err
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
