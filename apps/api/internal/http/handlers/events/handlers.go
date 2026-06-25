package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/audit"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB      *sql.DB
	Queries *generated.Queries
	Storage *storage.Client
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	cal, _, err := resolveCalendarMember(ctx, deps, calPubID, userID)
	return cal, err
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	cal, member, err := resolveCalendarMember(ctx, deps, calPubID, userID)
	if err != nil {
		return generated.Calendar{}, err
	}
	if member.Role == generated.CalendarMembersRoleViewer {
		return generated.Calendar{}, apierrors.CalendarRoleRequired
	}
	return cal, nil
}

func resolveCalendarMember(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	pubBytes, err := parseUUID(calPubID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.InternalUnexpected
	}
	member, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	})
	if err != nil {
		return generated.Calendar{}, generated.CalendarMember{}, apierrors.CalendarAccessDenied
	}
	return cal, member, nil
}

func mapRecurrenceRule(data *json.RawMessage) *RecurrenceRuleResponse {
	if data == nil {
		return nil
	}
	rule := recurrence.ParseRule(*data)
	if rule == nil {
		return nil
	}
	return &RecurrenceRuleResponse{
		Freq:       rule.Freq,
		Interval:   rule.Interval,
		ByDay:      rule.ByDay,
		ByMonthDay: rule.ByMonthDay,
		BySetPos:   rule.BySetPos,
		Until:      rule.Until,
		Count:      rule.Count,
	}
}

func nullInt32ToPtr(n sql.NullInt32) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int32)
	return &v
}

func ptrIntToNullInt32(p *int) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*p), Valid: true}
}

func mapEvent(e generated.Event, calPubID []byte) EventResponse {
	return EventResponse{
		ID:                 pubIDToHex(e.PublicID),
		CalendarID:         pubIDToHex(calPubID),
		Title:              e.Title,
		AllDay:             e.AllDay,
		StartAt:            e.StartAt,
		EndAt:              e.EndAt,
		Timezone:           e.Timezone,
		Color:              e.Color,
		Location:           e.Location,
		Memo:               e.Memo,
		URL:                e.Url,
		NotificationOffset: nullInt32ToPtr(e.NotificationOffset),
		Participants:       []string{},
		RecurrenceRule:     mapRecurrenceRule(e.RecurrenceRule),
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          e.UpdatedAt,
	}
}

func mapRecurringInstance(e generated.Event, calPubID []byte, occ recurrence.Occurrence) EventResponse {
	parentID := pubIDToHex(e.PublicID)
	dateStr := occ.StartAt.Format("20060102")
	return EventResponse{
		ID:                 fmt.Sprintf("%s_%s", parentID, dateStr),
		CalendarID:         pubIDToHex(calPubID),
		Title:              e.Title,
		AllDay:             e.AllDay,
		StartAt:            occ.StartAt,
		EndAt:              occ.EndAt,
		Color:              e.Color,
		Location:           e.Location,
		Memo:               e.Memo,
		URL:                e.Url,
		NotificationOffset: nullInt32ToPtr(e.NotificationOffset),
		Participants:       []string{},
		RecurrenceRule:     mapRecurrenceRule(e.RecurrenceRule),
		IsRecurrence:       true,
		RecurrenceDate:     &dateStr,
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          e.UpdatedAt,
	}
}

// parseCompositeID splits a recurring instance ID ("uuid_YYYYMMDD") into parent UUID and date.
// Returns empty strings if the ID is not composite.
func parseCompositeID(eventID string) (parentUUID string, dateStr string) {
	// UUID is 36 chars, separator is _, date is 8 chars = 45 total
	if len(eventID) == 45 && eventID[36] == '_' {
		return eventID[:36], eventID[37:]
	}
	return "", ""
}

func prepareRecurrence(ruleData *json.RawMessage, startAt time.Time, tz string) (*json.RawMessage, sql.NullTime) {
	if ruleData == nil || string(*ruleData) == "null" {
		return nil, sql.NullTime{}
	}
	rule := recurrence.ParseRule(*ruleData)
	if rule == nil {
		return nil, sql.NullTime{}
	}
	// Anchor the recurrence end in the event's timezone so DST does not shift
	// the boundary used by SQL range queries.
	end := recurrence.ComputeEndInZone(rule, startAt, tz)
	return ruleData, sql.NullTime{Time: end, Valid: true}
}

var validWeekdays = map[string]bool{
	"SU": true, "MO": true, "TU": true, "WE": true, "TH": true, "FR": true, "SA": true,
}

// validateRecurrenceRule rejects malformed recurrence rules so a typo cannot
// silently produce an event that never appears (unknown freq) or recurs forever
// (unparseable until). Returns nil when there is no rule.
func validateRecurrenceRule(data *json.RawMessage) *apierrors.Spec {
	if data == nil || len(*data) == 0 || string(*data) == "null" {
		return nil
	}
	rule := recurrence.ParseRule(*data)
	if rule == nil {
		return apierrors.BadRequest
	}
	switch rule.Freq {
	case "daily", "weekly", "monthly", "yearly":
	default:
		return apierrors.BadRequest
	}
	if rule.Interval < 1 || rule.Interval > 999 {
		return apierrors.BadRequest
	}
	if rule.Count < 0 || rule.Count > 1000 {
		return apierrors.BadRequest
	}
	if rule.ByMonthDay < 0 || rule.ByMonthDay > 31 {
		return apierrors.BadRequest
	}
	if rule.BySetPos < 0 || rule.BySetPos > 5 {
		return apierrors.BadRequest
	}
	for _, d := range rule.ByDay {
		if !validWeekdays[d] {
			return apierrors.BadRequest
		}
	}
	if rule.Until != nil {
		if _, e1 := time.Parse(time.RFC3339, *rule.Until); e1 != nil {
			if _, e2 := time.Parse("2006-01-02", *rule.Until); e2 != nil {
				return apierrors.BadRequest
			}
		}
	}
	// A monthly rule with byDay but no bySetPos is ambiguous and would be
	// silently ignored by the expander, so reject it rather than mislead.
	if rule.Freq == "monthly" && len(rule.ByDay) > 0 && rule.BySetPos == 0 {
		return apierrors.BadRequest
	}
	return nil
}

// resolveAssignee maps an assignee's public user ID to the member's internal ID,
// requiring that the user is actually a member of the calendar so a foreign user
// cannot be attached (cross-tenant leak). A nil/empty assignee clears it.
func resolveAssignee(ctx context.Context, deps Deps, calID uint32, assignedTo *string) (sql.NullInt32, *apierrors.Spec) {
	if assignedTo == nil || *assignedTo == "" {
		return sql.NullInt32{}, nil
	}
	pub, err := parseUUID(*assignedTo)
	if err != nil {
		return sql.NullInt32{}, apierrors.BadRequest
	}
	u, err := deps.Queries.GetUserByPublicID(ctx, pub)
	if err != nil {
		return sql.NullInt32{}, apierrors.BadRequest
	}
	if _, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: calID,
		UserID:     u.ID,
	}); err != nil {
		return sql.NullInt32{}, apierrors.BadRequest
	}
	return sql.NullInt32{Int32: int32(u.ID), Valid: true}, nil
}

// assigneePublicID resolves an internal assigned_to id back to a public user ID
// for responses, returning nil when unset or unresolvable.
func assigneePublicID(ctx context.Context, deps Deps, id sql.NullInt32) *string {
	if !id.Valid {
		return nil
	}
	u, err := deps.Queries.GetUserByID(ctx, uint32(id.Int32))
	if err != nil {
		return nil
	}
	s := pubIDToHex(u.PublicID)
	return &s
}

// creatorAvatarTTL is generous because event responses are cached client-side,
// so the presigned avatar URL must outlive a typical browsing session.
const creatorAvatarTTL = time.Hour

// userBrief is the public identity of a user (creator/assignee) for responses.
type userBrief struct {
	publicID  string
	name      string
	icon      string
	avatarURL string
}

// lookupUser resolves a user's public identity, memoizing in cache when provided
// so a list request does not re-query the same creator repeatedly.
func lookupUser(ctx context.Context, deps Deps, id uint32, cache map[uint32]userBrief) userBrief {
	if cache != nil {
		if b, ok := cache[id]; ok {
			return b
		}
	}
	var b userBrief
	if u, err := deps.Queries.GetUserByID(ctx, id); err == nil {
		b = userBrief{publicID: pubIDToHex(u.PublicID), name: u.Name, icon: u.Icon}
		if deps.Storage != nil && u.AvatarStorageKey.Valid && u.AvatarStorageKey.String != "" {
			if url, err := deps.Storage.PresignGet(ctx, u.AvatarStorageKey.String, creatorAvatarTTL); err == nil {
				b.avatarURL = url
			}
		}
	}
	if cache != nil {
		cache[id] = b
	}
	return b
}

// applyCreator copies an already-resolved brief onto resp.
func applyCreator(resp *EventResponse, b userBrief) {
	resp.CreatedBy = b.publicID
	resp.CreatorName = b.name
	resp.CreatorIcon = b.icon
	resp.CreatorAvatarURL = b.avatarURL
}

// setCreator fills the creator identity fields on resp.
func setCreator(ctx context.Context, deps Deps, resp *EventResponse, createdBy uint32, cache map[uint32]userBrief) {
	applyCreator(resp, lookupUser(ctx, deps, createdBy, cache))
}

// isMember reports whether userID belongs to the calendar — used to keep event
// participants scoped to calendar members.
func isMember(ctx context.Context, deps Deps, calID, userID uint32) bool {
	_, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: calID,
		UserID:     userID,
	})
	return err == nil
}

// exceptionInfo holds a single-occurrence override row for a recurring series.
type exceptionInfo struct {
	child     generated.Event
	cancelled bool
}

// loadExceptions returns the override rows for a recurring master keyed by the
// UTC millisecond of the original occurrence they replace, so the expander can
// substitute or skip individual instances.
func loadExceptions(ctx context.Context, deps Deps, parentID uint32) map[int64]exceptionInfo {
	rows, err := deps.Queries.ListRecurrenceExceptionsByParent(ctx, sql.NullInt32{Int32: int32(parentID), Valid: true})
	if err != nil || len(rows) == 0 {
		return nil
	}
	m := make(map[int64]exceptionInfo, len(rows))
	for _, r := range rows {
		if !r.RecurrenceOriginalStart.Valid {
			continue
		}
		m[r.RecurrenceOriginalStart.Time.UTC().UnixMilli()] = exceptionInfo{child: r, cancelled: r.RecurrenceCancelled}
	}
	return m
}

// mapExceptionInstance renders a modified occurrence using the override row's own
// fields while keeping the composite ID anchored to the original occurrence date,
// so a subsequent edit resolves back to the same override.
func mapExceptionInstance(master, child generated.Event, calPubID []byte, originalStart time.Time) EventResponse {
	parentID := pubIDToHex(master.PublicID)
	dateStr := originalStart.UTC().Format("20060102")
	return EventResponse{
		ID:                 fmt.Sprintf("%s_%s", parentID, dateStr),
		CalendarID:         pubIDToHex(calPubID),
		Title:              child.Title,
		AllDay:             child.AllDay,
		StartAt:            child.StartAt,
		EndAt:              child.EndAt,
		Timezone:           child.Timezone,
		Color:              child.Color,
		Location:           child.Location,
		Memo:               child.Memo,
		URL:                child.Url,
		NotificationOffset: nullInt32ToPtr(child.NotificationOffset),
		Participants:       []string{},
		RecurrenceRule:     mapRecurrenceRule(master.RecurrenceRule),
		IsRecurrence:       true,
		RecurrenceDate:     &dateStr,
		CreatedAt:          child.CreatedAt,
		UpdatedAt:          child.UpdatedAt,
	}
}

// occurrenceStartForDate resolves the exact UTC start instant of the occurrence
// falling on dateStr (YYYYMMDD), confirming it is a genuine occurrence of the
// rule rather than an arbitrary date crafted into a composite ID.
func occurrenceStartForDate(rule *recurrence.Rule, evt generated.Event, dateStr string) (time.Time, error) {
	instanceDate, err := time.Parse("20060102", dateStr)
	if err != nil {
		return time.Time{}, err
	}
	dayStart := time.Date(instanceDate.Year(), instanceDate.Month(), instanceDate.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt, evt.EndAt, dayStart, dayEnd, evt.Timezone) {
		if occ.StartAt.UTC().Format("20060102") == dateStr {
			return occ.StartAt.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("date %s is not an occurrence", dateStr)
}

func ListEvents(deps Deps) func(context.Context, *ListEventsInput) (*ListEventsOutput, error) {
	return func(ctx context.Context, in *ListEventsInput) (*ListEventsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var startTime, endTime time.Time
		if in.StartDate != "" && in.EndDate != "" {
			startTime, err = time.Parse("2006-01-02", in.StartDate)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			endTime, err = time.Parse("2006-01-02", in.EndDate)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			endTime = endTime.AddDate(0, 0, 1) // inclusive end
		} else {
			startTime = time.Now().AddDate(0, 0, -7)
			endTime = time.Now().AddDate(0, 0, in.Days)
		}

		// Fetch non-recurring events
		rows, err := deps.Queries.ListEventsByCalendarAndRange(ctx, generated.ListEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			StartAt:    endTime,
			EndAt:      startTime,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		creatorCache := map[uint32]userBrief{}
		var results []EventResponse
		for _, e := range rows {
			ev := mapEvent(e, cal.PublicID)
			ev.AssignedTo = assigneePublicID(ctx, deps, e.AssignedTo)
			setCreator(ctx, deps, &ev, e.CreatedBy, creatorCache)
			results = append(results, ev)
		}

		// Fetch and expand recurring events
		recurringRows, err := deps.Queries.ListRecurringEventsByCalendarAndRange(ctx, generated.ListRecurringEventsByCalendarAndRangeParams{
			CalendarID:    cal.ID,
			StartAt:       endTime,
			RecurrenceEnd: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		for _, e := range recurringRows {
			if e.RecurrenceRule == nil {
				continue
			}
			rule := recurrence.ParseRule(*e.RecurrenceRule)
			if rule == nil {
				continue
			}
			assignee := assigneePublicID(ctx, deps, e.AssignedTo)
			creator := lookupUser(ctx, deps, e.CreatedBy, creatorCache)
			exceptions := loadExceptions(ctx, deps, e.ID)
			consumed := make(map[int64]bool, len(exceptions))
			occurrences := recurrence.ExpandInZone(rule, e.StartAt, e.EndAt, startTime, endTime, e.Timezone)
			for _, occ := range occurrences {
				key := occ.StartAt.UTC().UnixMilli()
				if ex, ok := exceptions[key]; ok {
					consumed[key] = true
					if ex.cancelled {
						continue // this occurrence was deleted from the series
					}
					inst := mapExceptionInstance(e, ex.child, cal.PublicID, occ.StartAt)
					inst.AssignedTo = assigneePublicID(ctx, deps, ex.child.AssignedTo)
					applyCreator(&inst, creator)
					results = append(results, inst)
					continue
				}
				inst := mapRecurringInstance(e, cal.PublicID, occ)
				inst.AssignedTo = assignee
				applyCreator(&inst, creator)
				results = append(results, inst)
			}
			// Modified overrides whose original occurrence no longer exists (e.g.
			// the series rule or time changed) still surface at their own time so
			// the user's edit is not silently lost.
			for key, ex := range exceptions {
				if consumed[key] || ex.cancelled {
					continue
				}
				if ex.child.StartAt.Before(startTime) || !ex.child.StartAt.Before(endTime) {
					continue
				}
				inst := mapExceptionInstance(e, ex.child, cal.PublicID, ex.child.RecurrenceOriginalStart.Time.UTC())
				inst.AssignedTo = assigneePublicID(ctx, deps, ex.child.AssignedTo)
				applyCreator(&inst, creator)
				results = append(results, inst)
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].StartAt.Before(results[j].StartAt)
		})

		out := &ListEventsOutput{Body: results}
		if out.Body == nil {
			out.Body = []EventResponse{}
		}
		return out, nil
	}
}

func GetEvent(deps Deps) func(context.Context, *GetEventInput) (*GetEventOutput, error) {
	return func(ctx context.Context, in *GetEventInput) (*GetEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Check for composite ID (recurring instance)
		parentUUID, dateStr := parseCompositeID(in.EventID)
		if parentUUID != "" {
			evtPub, err := parseUUID(parentUUID)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
			if err != nil || evt.CalendarID != cal.ID {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			if evt.RecurrenceRule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			instanceDate, err := time.Parse("20060102", dateStr)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			// Confirm the requested date is an actual occurrence of the rule
			// rather than an arbitrary date crafted into the composite ID.
			dayStart := time.Date(instanceDate.Year(), instanceDate.Month(), instanceDate.Day(), 0, 0, 0, 0, time.UTC)
			dayEnd := dayStart.AddDate(0, 0, 1)
			var matched *recurrence.Occurrence
			for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt, evt.EndAt, dayStart, dayEnd, evt.Timezone) {
				if occ.StartAt.UTC().Format("20060102") == dateStr {
					o := occ
					matched = &o
					break
				}
			}
			if matched == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			// Honor a single-occurrence override if one exists for this instant.
			ex, err := deps.Queries.GetRecurrenceException(ctx, generated.GetRecurrenceExceptionParams{
				RecurrenceParentID:      sql.NullInt32{Int32: int32(evt.ID), Valid: true},
				RecurrenceOriginalStart: sql.NullTime{Time: matched.StartAt.UTC(), Valid: true},
			})
			if err == nil {
				if ex.RecurrenceCancelled {
					return nil, apierrors.ToHuma(apierrors.EventNotFound)
				}
				resp := mapExceptionInstance(evt, ex, cal.PublicID, matched.StartAt)
				resp.AssignedTo = assigneePublicID(ctx, deps, ex.AssignedTo)
				setCreator(ctx, deps, &resp, evt.CreatedBy, nil)
				return &GetEventOutput{Body: resp}, nil
			}

			resp := mapRecurringInstance(evt, cal.PublicID, *matched)
			resp.AssignedTo = assigneePublicID(ctx, deps, evt.AssignedTo)
			setCreator(ctx, deps, &resp, evt.CreatedBy, nil)
			return &GetEventOutput{Body: resp}, nil
		}

		evtPub, err := parseUUID(in.EventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		if evt.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.EventAccessDenied)
		}

		resp := mapEvent(evt, cal.PublicID)
		resp.AssignedTo = assigneePublicID(ctx, deps, evt.AssignedTo)
		setCreator(ctx, deps, &resp, evt.CreatedBy, nil)
		participants, _ := deps.Queries.ListEventParticipants(ctx, evt.ID)
		if len(participants) > 0 {
			pids := make([]string, 0, len(participants))
			for _, p := range participants {
				pids = append(pids, pubIDToHex(p.UserPublicID))
			}
			resp.Participants = pids
		}
		return &GetEventOutput{Body: resp}, nil
	}
}

func CreateEvent(deps Deps) func(context.Context, *CreateEventInput) (*CreateEventOutput, error) {
	return func(ctx context.Context, in *CreateEventInput) (*CreateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		startAt, err := time.Parse(time.RFC3339, in.Body.StartAt)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		endAt, err := time.Parse(time.RFC3339, in.Body.EndAt)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if endAt.Before(startAt) {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if spec := validateRecurrenceRule(in.Body.RecurrenceRule); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		pubID, _ := uuid.NewV7()
		color := in.Body.Color
		if color == "" {
			color = "#47B2F7"
		}

		tz := in.Body.Timezone
		if tz == "" {
			tz = "UTC"
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, tz)

		assignedTo, spec := resolveAssignee(ctx, deps, cal.ID, in.Body.AssignedTo)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		result, err := deps.Queries.CreateEvent(ctx, generated.CreateEventParams{
			PublicID:           pubID[:],
			CalendarID:         cal.ID,
			Title:              in.Body.Title,
			AllDay:             in.Body.AllDay,
			StartAt:            startAt,
			EndAt:              endAt,
			Timezone:           tz,
			Color:              color,
			Location:           in.Body.Location,
			Memo:               in.Body.Memo,
			Url:                in.Body.URL,
			CreatedBy:          userID,
			AssignedTo:         assignedTo,
			NotificationOffset: ptrIntToNullInt32(in.Body.NotificationOffset),
			RecurrenceRule:     ruleData,
			RecurrenceEnd:      recEnd,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		participants := []string{}
		if len(in.Body.Participants) > 0 {
			eventID64, _ := result.LastInsertId()
			eventID := uint32(eventID64)
			for _, pUUID := range in.Body.Participants {
				pPub, err := parseUUID(pUUID)
				if err != nil {
					continue
				}
				u, err := deps.Queries.GetUserByPublicID(ctx, pPub)
				if err != nil {
					continue
				}
				// Only calendar members may be added as participants, so a
				// foreign user's name/avatar cannot be attached and leaked.
				if !isMember(ctx, deps, cal.ID, u.ID) {
					continue
				}
				_ = deps.Queries.AddEventParticipant(ctx, generated.AddEventParticipantParams{
					EventID: eventID,
					UserID:  u.ID,
				})
				participants = append(participants, pUUID)
			}
		}

		resp := EventResponse{
			ID:                 pubID.String(),
			CalendarID:         pubIDToHex(cal.PublicID),
			Title:              in.Body.Title,
			AllDay:             in.Body.AllDay,
			StartAt:            startAt,
			EndAt:              endAt,
			Timezone:           tz,
			Color:              color,
			Location:           in.Body.Location,
			Memo:               in.Body.Memo,
			URL:                in.Body.URL,
			NotificationOffset: in.Body.NotificationOffset,
			Participants:       participants,
			AssignedTo:         assigneePublicID(ctx, deps, assignedTo),
			RecurrenceRule:     mapRecurrenceRule(ruleData),
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		setCreator(ctx, deps, &resp, userID, nil)

		newID64, _ := result.LastInsertId()
		audit.Record(ctx, deps.Queries, cal.ID, uint32(newID64), pubID[:], audit.EntityEvent, audit.ActionCreate, userID, in.Body.Title)

		return &CreateEventOutput{Body: resp}, nil
	}
}

func UpdateEvent(deps Deps) func(context.Context, *UpdateEventInput) (*UpdateEventOutput, error) {
	return func(ctx context.Context, in *UpdateEventInput) (*UpdateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// For composite IDs (recurring instances), resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		evtPub, err := parseUUID(eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
		if err != nil || evt.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		startAt, err := time.Parse(time.RFC3339, in.Body.StartAt)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		endAt, err := time.Parse(time.RFC3339, in.Body.EndAt)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if endAt.Before(startAt) {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		if spec := validateRecurrenceRule(in.Body.RecurrenceRule); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		tz := in.Body.Timezone
		if tz == "" {
			tz = evt.Timezone
			if tz == "" {
				tz = "UTC"
			}
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, tz)

		assignedTo, spec := resolveAssignee(ctx, deps, cal.ID, in.Body.AssignedTo)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		// Single-occurrence edit: write an override row instead of mutating the
		// whole series, so editing one instance no longer rewrites every instance.
		if isOccurrence && in.Scope == "this" && evt.RecurrenceRule != nil {
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			originalStart, oerr := occurrenceStartForDate(rule, evt, occurrenceDate)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			pubID, _ := uuid.NewV7()
			parentRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
			originalRef := sql.NullTime{Time: originalStart, Valid: true}
			if _, err := deps.Queries.UpsertRecurrenceException(ctx, generated.UpsertRecurrenceExceptionParams{
				PublicID:                pubID[:],
				CalendarID:              cal.ID,
				Title:                   in.Body.Title,
				AllDay:                  in.Body.AllDay,
				StartAt:                 startAt,
				EndAt:                   endAt,
				Timezone:                tz,
				Color:                   in.Body.Color,
				Location:                in.Body.Location,
				Memo:                    in.Body.Memo,
				Url:                     in.Body.URL,
				CreatedBy:               userID,
				AssignedTo:              assignedTo,
				NotificationOffset:      ptrIntToNullInt32(in.Body.NotificationOffset),
				RecurrenceParentID:      parentRef,
				RecurrenceOriginalStart: originalRef,
				RecurrenceCancelled:     false,
			}); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			child, err := deps.Queries.GetRecurrenceException(ctx, generated.GetRecurrenceExceptionParams{
				RecurrenceParentID:      parentRef,
				RecurrenceOriginalStart: originalRef,
			})
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			resp := mapExceptionInstance(evt, child, cal.PublicID, originalStart)
			resp.AssignedTo = assigneePublicID(ctx, deps, child.AssignedTo)
			setCreator(ctx, deps, &resp, evt.CreatedBy, nil)

			audit.Record(ctx, deps.Queries, cal.ID, evt.ID, evt.PublicID, audit.EntityEvent, audit.ActionUpdate, userID, in.Body.Title+" (occurrence)")

			return &UpdateEventOutput{Body: resp}, nil
		}

		err = deps.Queries.UpdateEvent(ctx, generated.UpdateEventParams{
			Title:              in.Body.Title,
			AllDay:             in.Body.AllDay,
			StartAt:            startAt,
			EndAt:              endAt,
			Timezone:           tz,
			Color:              in.Body.Color,
			Location:           in.Body.Location,
			Memo:               in.Body.Memo,
			Url:                in.Body.URL,
			AssignedTo:         assignedTo,
			NotificationOffset: ptrIntToNullInt32(in.Body.NotificationOffset),
			RecurrenceRule:     ruleData,
			RecurrenceEnd:      recEnd,
			ID:                 evt.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Replace participants
		_ = deps.Queries.DeleteAllEventParticipants(ctx, evt.ID)
		participants := []string{}
		for _, pUUID := range in.Body.Participants {
			pPub, err := parseUUID(pUUID)
			if err != nil {
				continue
			}
			u, err := deps.Queries.GetUserByPublicID(ctx, pPub)
			if err != nil {
				continue
			}
			// Only calendar members may be added as participants.
			if !isMember(ctx, deps, cal.ID, u.ID) {
				continue
			}
			_ = deps.Queries.AddEventParticipant(ctx, generated.AddEventParticipantParams{
				EventID: evt.ID,
				UserID:  u.ID,
			})
			participants = append(participants, pUUID)
		}

		updated, _ := deps.Queries.GetEventByPublicID(ctx, evtPub)
		resp := mapEvent(updated, cal.PublicID)
		resp.Participants = participants
		resp.AssignedTo = assigneePublicID(ctx, deps, updated.AssignedTo)
		setCreator(ctx, deps, &resp, updated.CreatedBy, nil)

		audit.Record(ctx, deps.Queries, cal.ID, evt.ID, evt.PublicID, audit.EntityEvent, audit.ActionUpdate, userID, in.Body.Title)

		return &UpdateEventOutput{Body: resp}, nil
	}
}

func DeleteEvent(deps Deps) func(context.Context, *DeleteEventInput) (*DeleteEventOutput, error) {
	return func(ctx context.Context, in *DeleteEventInput) (*DeleteEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// For composite IDs, resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		evtPub, err := parseUUID(eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
		if err != nil || evt.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		// Single-occurrence delete: record a cancellation tombstone so only this
		// instance disappears while the rest of the series is preserved.
		if isOccurrence && in.Scope == "this" && evt.RecurrenceRule != nil {
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			originalStart, oerr := occurrenceStartForDate(rule, evt, occurrenceDate)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			pubID, _ := uuid.NewV7()
			if _, err := deps.Queries.UpsertRecurrenceException(ctx, generated.UpsertRecurrenceExceptionParams{
				PublicID:                pubID[:],
				CalendarID:              cal.ID,
				Title:                   evt.Title,
				AllDay:                  evt.AllDay,
				StartAt:                 originalStart,
				EndAt:                   originalStart.Add(evt.EndAt.Sub(evt.StartAt)),
				Timezone:                evt.Timezone,
				Color:                   evt.Color,
				Location:                evt.Location,
				Memo:                    evt.Memo,
				Url:                     evt.Url,
				CreatedBy:               userID,
				RecurrenceParentID:      sql.NullInt32{Int32: int32(evt.ID), Valid: true},
				RecurrenceOriginalStart: sql.NullTime{Time: originalStart, Valid: true},
				RecurrenceCancelled:     true,
			}); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}

			audit.Record(ctx, deps.Queries, cal.ID, evt.ID, evt.PublicID, audit.EntityEvent, audit.ActionDelete, userID, evt.Title+" (occurrence)")

			return &DeleteEventOutput{}, nil
		}

		// Whole-series delete: removing the master cascades to its override rows.
		err = deps.Queries.DeleteEvent(ctx, evt.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		audit.Record(ctx, deps.Queries, cal.ID, evt.ID, evt.PublicID, audit.EntityEvent, audit.ActionDelete, userID, evt.Title)

		return &DeleteEventOutput{}, nil
	}
}

func ListComments(deps Deps) func(context.Context, *ListCommentsInput) (*ListCommentsOutput, error) {
	return func(ctx context.Context, in *ListCommentsInput) (*ListCommentsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// For composite IDs, resolve to parent for comments
		eventID := in.EventID
		if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
			eventID = parentUUID
		}

		evtPub, err := parseUUID(eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
		if err != nil || evt.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		rows, err := deps.Queries.ListEventComments(ctx, evt.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListCommentsOutput{Body: make([]CommentResponse, 0, len(rows))}
		for _, c := range rows {
			out.Body = append(out.Body, CommentResponse{
				ID:           pubIDToHex(c.PublicID),
				UserPublicID: pubIDToHex(c.UserPublicID),
				UserName:     c.UserName,
				UserIcon:     c.UserIcon,
				Body:         c.Body,
				CreatedAt:    c.CreatedAt,
			})
		}
		return out, nil
	}
}

func CreateComment(deps Deps) func(context.Context, *CreateCommentInput) (*CreateCommentOutput, error) {
	return func(ctx context.Context, in *CreateCommentInput) (*CreateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// For composite IDs, resolve to parent for comments
		eventID := in.EventID
		if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
			eventID = parentUUID
		}

		evtPub, err := parseUUID(eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}
		evt, err := deps.Queries.GetEventByPublicID(ctx, evtPub)
		if err != nil || evt.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		pubID, _ := uuid.NewV7()
		_, err = deps.Queries.CreateEventComment(ctx, generated.CreateEventCommentParams{
			PublicID: pubID[:],
			EventID:  evt.ID,
			UserID:   userID,
			Body:     in.Body.Content,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		user, _ := deps.Queries.GetUserByID(ctx, userID)

		out := &CreateCommentOutput{}
		out.Body = CommentResponse{
			ID:           pubID.String(),
			UserPublicID: pubIDToHex(user.PublicID),
			UserName:     user.Name,
			UserIcon:     user.Icon,
			Body:         in.Body.Content,
			CreatedAt:    time.Now(),
		}
		return out, nil
	}
}

func UpdateComment(deps Deps) func(context.Context, *UpdateCommentInput) (*UpdateCommentOutput, error) {
	return func(ctx context.Context, in *UpdateCommentInput) (*UpdateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		commentPub, err := parseUUID(in.CommentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		comment, err := deps.Queries.GetEventCommentByPublicID(ctx, commentPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		if comment.UserID != userID {
			return nil, apierrors.ToHuma(apierrors.CommentAccessDenied)
		}

		err = deps.Queries.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
			Body: in.Body.Content,
			ID:   comment.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &UpdateCommentOutput{}
		out.Body = CommentResponse{
			ID:           pubIDToHex(comment.PublicID),
			UserPublicID: pubIDToHex(comment.UserPublicID),
			UserName:     comment.UserName,
			UserIcon:     comment.UserIcon,
			Body:         in.Body.Content,
			CreatedAt:    comment.CreatedAt,
		}
		return out, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		commentPub, err := parseUUID(in.CommentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		comment, err := deps.Queries.GetEventCommentByPublicID(ctx, commentPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		if comment.UserID != userID {
			return nil, apierrors.ToHuma(apierrors.CommentAccessDenied)
		}

		err = deps.Queries.DeleteEventComment(ctx, comment.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteCommentOutput{}, nil
	}
}
