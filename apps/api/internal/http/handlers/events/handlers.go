package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/eventexpand"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/recurrence"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB                *sql.DB
	Queries           *generated.Queries
	Storage           *storage.Client
	WorkspaceID       uint32
	WorkspacePublicID []byte
}

// defaultEventColor is used when the owner has no colour on the calendar,
// which cannot normally happen — the column has a default — but a response
// still has to render something.
const defaultEventColor = "#47B2F7"

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Write(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

func resolveCalendarMember(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	return calresolve.Member(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// toAPIError maps an apierrors.Spec returned by a helper onto a huma error,
// falling back to an internal error for anything unexpected.
func toAPIError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
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

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullStringValue(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

func nullTimeValue(n sql.NullTime) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return n.Time
}

// showAsOrDefault keeps the iCalendar TRANSP axis to its own vocabulary. An
// unrecognised value falls back to busy rather than being stored verbatim:
// this column is what external free/busy consumers read, and a value they
// do not know is worse than the conservative answer.
func showAsOrDefault(s string) generated.CalendarEventsShowAs {
	switch generated.CalendarEventsShowAs(s) {
	case generated.CalendarEventsShowAsFree:
		return generated.CalendarEventsShowAsFree
	case generated.CalendarEventsShowAsTentative:
		return generated.CalendarEventsShowAsTentative
	case generated.CalendarEventsShowAsOof:
		return generated.CalendarEventsShowAsOof
	default:
		return generated.CalendarEventsShowAsBusy
	}
}

// flexibilityOrDefault defaults to fixed. Assuming an unspecified
// commitment is movable would advertise availability its owner never
// agreed to.
func flexibilityOrDefault(s string) generated.CalendarEventsFlexibility {
	switch generated.CalendarEventsFlexibility(s) {
	case generated.CalendarEventsFlexibilityNegotiable:
		return generated.CalendarEventsFlexibilityNegotiable
	case generated.CalendarEventsFlexibilityConditional:
		return generated.CalendarEventsFlexibilityConditional
	default:
		return generated.CalendarEventsFlexibilityFixed
	}
}

// colorForOwner resolves the colour an event renders in. The colour lives
// on the membership, not the event: an event sits on its owner's layer, and
// the layer is what carries a colour the whole calendar has agreed on.
func colorForOwner(ctx context.Context, deps Deps, calID, ownerID uint32, cache map[uint32]string) string {
	if cache != nil {
		if c, ok := cache[ownerID]; ok {
			return c
		}
	}
	color := defaultEventColor
	if m, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: calID,
		UserID:     ownerID,
	}); err == nil && m.MemberColor != "" {
		color = m.MemberColor
	}
	if cache != nil {
		cache[ownerID] = color
	}
	return color
}

func mapEvent(e generated.CalendarEvent, calPubID []byte) EventResponse {
	return EventResponse{
		ID:                 pubIDToHex(e.PublicID),
		CalendarID:         pubIDToHex(calPubID),
		Title:              e.Title,
		AllDay:             e.AllDay,
		StartAt:            nullTimeValue(e.StartAt),
		EndAt:              nullTimeValue(e.EndAt),
		Timezone:           e.Timezone,
		Location:           nullStringValue(e.Location),
		Memo:               nullStringValue(e.Memo),
		URL:                nullStringValue(e.URL),
		ShowAs:             string(e.ShowAs),
		Flexibility:        string(e.Flexibility),
		NotificationOffset: nullInt32ToPtr(e.NotificationOffset),
		Participants:       []string{},
		RecurrenceRule:     mapRecurrenceRule(e.RecurrenceRule),
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          nullTimeValue(e.UpdatedAt),
	}
}

func mapRecurringInstance(e generated.CalendarEvent, calPubID []byte, occ recurrence.Occurrence) EventResponse {
	resp := mapEvent(e, calPubID)
	dateStr := occ.StartAt.Format("20060102")
	resp.ID = fmt.Sprintf("%s_%s", pubIDToHex(e.PublicID), dateStr)
	resp.StartAt = occ.StartAt
	resp.EndAt = occ.EndAt
	resp.IsRecurrence = true
	resp.RecurrenceDate = &dateStr
	return resp
}

// mapOverrideInstance renders a changed occurrence using the override row's
// own fields while keeping the composite ID anchored to the original
// occurrence date, so a subsequent edit resolves back to the same override.
func mapOverrideInstance(master, child generated.CalendarEvent, calPubID []byte, originalStart time.Time) EventResponse {
	resp := mapEvent(child, calPubID)
	dateStr := originalStart.UTC().Format("20060102")
	resp.ID = fmt.Sprintf("%s_%s", pubIDToHex(master.PublicID), dateStr)
	// The rule belongs to the series; the override row deliberately carries
	// none of its own, so read it off the master.
	resp.RecurrenceRule = mapRecurrenceRule(master.RecurrenceRule)
	resp.IsRecurrence = true
	resp.RecurrenceDate = &dateStr
	return resp
}

func participantPublicIDs(ctx context.Context, deps Deps, eventID uint32) []string {
	rows, err := deps.Queries.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(eventID), Valid: true})
	if err != nil || len(rows) == 0 {
		return []string{}
	}
	ids := make([]string, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, pubIDToHex(p.UserPublicID))
	}
	return ids
}

type eventParticipant struct {
	publicID string
	userID   uint32
}

func validateEventParticipants(ctx context.Context, q *generated.Queries, calID uint32, participantIDs []string) ([]eventParticipant, *apierrors.Spec) {
	participants := make([]eventParticipant, 0, len(participantIDs))
	seen := make(map[uint32]struct{}, len(participantIDs))
	for _, pUUID := range participantIDs {
		pPub, err := parseUUID(pUUID)
		if err != nil {
			return nil, apierrors.BadRequest
		}
		u, err := q.GetUserByPublicID(ctx, pPub)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.BadRequest
			}
			return nil, apierrors.InternalUnexpected
		}
		// Only calendar members may be added as participants.
		if _, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{CalendarID: calID, UserID: u.ID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.BadRequest
			}
			return nil, apierrors.InternalUnexpected
		}
		if _, duplicate := seen[u.ID]; duplicate {
			continue
		}
		seen[u.ID] = struct{}{}
		participants = append(participants, eventParticipant{publicID: pubIDToHex(u.PublicID), userID: u.ID})
	}
	return participants, nil
}

func replaceEventParticipants(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, participants []eventParticipant) error {
	// Disable every current row first, then revive the ones still wanted.
	// A hard delete would drop the RSVP of somebody who is being re-added in
	// the same request, silently resetting their answer to pending.
	if err := q.RemoveAllEventAttendees(ctx, sql.NullInt32{Int32: int32(eventID), Valid: true}); err != nil {
		return err
	}
	for _, participant := range participants {
		pubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.AddEventAttendee(ctx, generated.AddEventAttendeeParams{
			PublicID:    pubID[:],
			WorkspaceID: workspaceID,
			EventID:     sql.NullInt32{Int32: int32(eventID), Valid: true},
			UserID:      participant.userID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func participantPublicIDList(participants []eventParticipant) []string {
	ids := make([]string, 0, len(participants))
	for _, participant := range participants {
		ids = append(ids, participant.publicID)
	}
	return ids
}

// parseCompositeID splits a recurring instance ID ("uuid_YYYYMMDD") into parent UUID and date.
// Returns empty strings if the ID is not composite.
func parseCompositeID(eventID string) (parentUUID string, dateStr string) {
	return calresolve.SplitCompositeID(eventID)
}

// validateTimezone rejects a timezone that Go's zone database cannot load, so an
// invalid IANA name (e.g. a typo like "America/New_Yrok") fails loudly with a
// BadRequest instead of silently falling back to UTC.
func validateTimezone(tz string) *apierrors.Spec {
	if _, err := time.LoadLocation(tz); err != nil {
		return apierrors.BadRequest
	}
	return nil
}

func prepareRecurrence(ruleData *json.RawMessage, startAt, endAt time.Time, tz string) (*json.RawMessage, sql.NullTime) {
	if ruleData == nil || string(*ruleData) == "null" {
		return nil, sql.NullTime{}
	}
	rule := recurrence.ParseRule(*ruleData)
	if rule == nil {
		return nil, sql.NullTime{}
	}
	// Anchor the recurrence end in the event's timezone so DST does not shift
	// the boundary used by SQL range queries.
	end := recurrence.ComputeEndInZone(rule, startAt, endAt, tz)
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

// resolveOwner maps a public user ID onto the member's internal ID,
// requiring that the user actually belongs to the calendar so an event
// cannot be filed under somebody outside it. A nil owner falls back to
// fallbackUserID, which is the acting user: the contract requires an owner
// on every event, and whoever put it there is the honest answer.
func resolveOwner(ctx context.Context, deps Deps, calID uint32, ownerID *string, fallbackUserID uint32) (uint32, *apierrors.Spec) {
	if ownerID == nil || *ownerID == "" {
		return fallbackUserID, nil
	}
	pub, err := parseUUID(*ownerID)
	if err != nil {
		return 0, apierrors.BadRequest
	}
	u, err := deps.Queries.GetUserByPublicID(ctx, pub)
	if err != nil {
		return 0, apierrors.BadRequest
	}
	if _, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: calID,
		UserID:     u.ID,
	}); err != nil {
		return 0, apierrors.BadRequest
	}
	return u.ID, nil
}

// creatorAvatarTTL is generous because event responses are cached client-side,
// so the presigned avatar URL must outlive a typical browsing session.
const creatorAvatarTTL = time.Hour

// userBrief is the public identity of a user (creator/owner) for responses.
type userBrief struct {
	publicID  string
	name      string
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
		b = userBrief{publicID: pubIDToHex(u.PublicID), name: u.DisplayName}
		b.avatarURL = avatarURLFor(ctx, deps, u)
	}
	if cache != nil {
		cache[id] = b
	}
	return b
}

// avatarURLFor prefers an uploaded avatar over an external one: a user who
// has uploaded a picture has said which they want, and the provider URL
// they signed up with may since have gone stale.
func avatarURLFor(ctx context.Context, deps Deps, u generated.User) string {
	if deps.Storage != nil && u.AvatarStorageObjectID.Valid {
		if obj, err := deps.Queries.GetStorageObjectByID(ctx, uint32(u.AvatarStorageObjectID.Int32)); err == nil {
			if url, err := deps.Storage.PresignGet(ctx, obj.StorageKey, creatorAvatarTTL); err == nil {
				return url
			}
		}
	}
	return nullStringValue(u.AvatarURL)
}

// applyCreator copies an already-resolved brief onto resp.
func applyCreator(resp *EventResponse, b userBrief) {
	resp.CreatedBy = b.publicID
	resp.CreatorName = b.name
	resp.CreatorAvatarURL = b.avatarURL
}

// setCreator fills the creator identity fields on resp.
func setCreator(ctx context.Context, deps Deps, resp *EventResponse, createdBy uint32, cache map[uint32]userBrief) {
	applyCreator(resp, lookupUser(ctx, deps, createdBy, cache))
}

// decorate fills in the fields that are not columns on the event row: the
// owner's public id, the colour their membership carries, and the creator's
// identity.
func decorate(ctx context.Context, deps Deps, resp *EventResponse, e generated.CalendarEvent, calID uint32, users map[uint32]userBrief, colors map[uint32]string) {
	resp.OwnerID = lookupUser(ctx, deps, e.OwnerUserID, users).publicID
	resp.Color = colorForOwner(ctx, deps, calID, e.OwnerUserID, colors)
	setCreator(ctx, deps, resp, e.CreatedByUserID, users)
}

// occurrenceStartForDate resolves the exact UTC start instant of the occurrence
// falling on dateStr (YYYYMMDD), confirming it is a genuine occurrence of the
// rule rather than an arbitrary date crafted into a composite ID.
func occurrenceStartForDate(rule *recurrence.Rule, evt generated.CalendarEvent, dateStr string) (time.Time, error) {
	if !evt.StartAt.Valid || !evt.EndAt.Valid {
		return time.Time{}, fmt.Errorf("event has no dates to expand")
	}
	instanceDate, err := time.Parse("20060102", dateStr)
	if err != nil {
		return time.Time{}, err
	}
	dayStart := time.Date(instanceDate.Year(), instanceDate.Month(), instanceDate.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt.Time, evt.EndAt.Time, dayStart, dayEnd, evt.Timezone) {
		if occ.StartAt.UTC().Format("20060102") == dateStr {
			return occ.StartAt.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("date %s is not an occurrence", dateStr)
}

// loadEventInCalendar resolves an event public id and confirms it belongs to
// the calendar the caller proved access to, so an id from another calendar
// cannot be reached by guessing.
func loadEventInCalendar(ctx context.Context, deps Deps, calID uint32, eventPublicID string) (generated.CalendarEvent, error) {
	evtPub, err := parseUUID(eventPublicID)
	if err != nil {
		return generated.CalendarEvent{}, apierrors.EventNotFound
	}
	evt, err := deps.Queries.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: deps.WorkspaceID,
		PublicID:    evtPub,
	})
	if err != nil || evt.CalendarID != calID {
		return generated.CalendarEvent{}, apierrors.EventNotFound
	}
	return evt, nil
}

func ListEvents(deps Deps) func(context.Context, *ListEventsInput) (*ListEventsOutput, error) {
	return func(ctx context.Context, in *ListEventsInput) (*ListEventsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
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

		rows, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		userCache := map[uint32]userBrief{}
		colorCache := map[uint32]string{}
		var results []EventResponse
		for _, e := range rows {
			ev := mapEvent(e, cal.PublicID)
			ev.Participants = participantPublicIDs(ctx, deps, e.ID)
			decorate(ctx, deps, &ev, e, cal.ID, userCache, colorCache)
			results = append(results, ev)
		}

		recurringRows, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		for _, e := range recurringRows {
			masterParticipants := participantPublicIDs(ctx, deps, e.ID)
			for _, expanded := range eventexpand.ExpandRecurringEvent(ctx, deps.Queries, e, startTime, endTime) {
				if expanded.IsOverride {
					inst := mapOverrideInstance(e, expanded.Event, cal.PublicID, expanded.OriginalStart)
					inst.Participants = participantPublicIDs(ctx, deps, expanded.Event.ID)
					decorate(ctx, deps, &inst, expanded.Event, cal.ID, userCache, colorCache)
					results = append(results, inst)
					continue
				}
				inst := mapRecurringInstance(e, cal.PublicID, expanded.Occurrence)
				inst.Participants = masterParticipants
				decorate(ctx, deps, &inst, e, cal.ID, userCache, colorCache)
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
			return nil, toAPIError(err)
		}

		// Check for composite ID (recurring instance)
		parentUUID, dateStr := parseCompositeID(in.EventID)
		if parentUUID != "" {
			evt, err := loadEventInCalendar(ctx, deps, cal.ID, parentUUID)
			if err != nil {
				return nil, toAPIError(err)
			}

			instanceDate, perr := time.Parse("20060102", dateStr)
			if perr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			dayStart := time.Date(instanceDate.Year(), instanceDate.Month(), instanceDate.Day(), 0, 0, 0, 0, time.UTC)
			dayEnd := dayStart.AddDate(0, 0, 1)
			for _, expanded := range eventexpand.ExpandRecurringEvent(ctx, deps.Queries, evt, dayStart, dayEnd) {
				if expanded.OriginalStart.Format("20060102") != dateStr {
					continue
				}
				var resp EventResponse
				source := evt
				if expanded.IsOverride {
					resp = mapOverrideInstance(evt, expanded.Event, cal.PublicID, expanded.OriginalStart)
					source = expanded.Event
				} else {
					resp = mapRecurringInstance(evt, cal.PublicID, expanded.Occurrence)
				}
				resp.Participants = participantPublicIDs(ctx, deps, source.ID)
				decorate(ctx, deps, &resp, source, cal.ID, nil, nil)
				return &GetEventOutput{Body: resp}, nil
			}
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		resp := mapEvent(evt, cal.PublicID)
		resp.Participants = participantPublicIDs(ctx, deps, evt.ID)
		decorate(ctx, deps, &resp, evt, cal.ID, nil, nil)
		return &GetEventOutput{Body: resp}, nil
	}
}

func CreateEvent(deps Deps) func(context.Context, *CreateEventInput) (*CreateEventOutput, error) {
	return func(ctx context.Context, in *CreateEventInput) (*CreateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
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

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		tz := in.Body.Timezone
		if tz == "" {
			tz = "UTC"
		}
		if spec := validateTimezone(tz); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, endAt, tz)

		ownerID, spec := resolveOwner(ctx, deps, cal.ID, in.Body.OwnerID, userID)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		participants, spec := validateEventParticipants(ctx, deps.Queries, cal.ID, in.Body.Participants)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		var created generated.CalendarEvent
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			result, err := q.CreateCalendarEvent(ctx, generated.CreateCalendarEventParams{
				PublicID:           pubID[:],
				WorkspaceID:        deps.WorkspaceID,
				CalendarID:         cal.ID,
				Kind:               generated.CalendarEventsKindEvent,
				Visibility:         generated.CalendarEventsVisibilityDefault,
				ShowAs:             showAsOrDefault(in.Body.ShowAs),
				Flexibility:        flexibilityOrDefault(in.Body.Flexibility),
				Title:              in.Body.Title,
				AllDay:             in.Body.AllDay,
				StartAt:            sql.NullTime{Time: startAt, Valid: true},
				EndAt:              sql.NullTime{Time: endAt, Valid: true},
				Timezone:           tz,
				Location:           nullString(in.Body.Location),
				Memo:               nullString(in.Body.Memo),
				URL:                nullString(in.Body.URL),
				OwnerUserID:        ownerID,
				CreatedByUserID:    userID,
				NotificationOffset: ptrIntToNullInt32(in.Body.NotificationOffset),
				RecurrenceRule:     ruleData,
				RecurrenceEnd:      recEnd,
			})
			if err != nil {
				return err
			}
			eventID64, err := result.LastInsertId()
			if err != nil {
				return err
			}
			eventID := uint32(eventID64)
			if err := replaceEventParticipants(ctx, q, deps.WorkspaceID, eventID, participants); err != nil {
				return err
			}
			// Reload the stored row so the response reflects DB truth (normalized
			// datetimes, DB-assigned created_at/updated_at) rather than time.Now().
			created, err = q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				PublicID:    pubID[:],
			})
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeEventCreated,
				Summary:     in.Body.Title,
				Subject:     pubID[:],
			})
		})
		if err != nil {
			slog.ErrorContext(ctx, "failed to create event", "calendarID", cal.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		resp := mapEvent(created, cal.PublicID)
		resp.Participants = participantPublicIDList(participants)
		decorate(ctx, deps, &resp, created, cal.ID, nil, nil)

		return &CreateEventOutput{Body: resp}, nil
	}
}

func UpdateEvent(deps Deps) func(context.Context, *UpdateEventInput) (*UpdateEventOutput, error) {
	return func(ctx context.Context, in *UpdateEventInput) (*UpdateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// For composite IDs (recurring instances), resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, eventID)
		if err != nil {
			return nil, toAPIError(err)
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
		if spec := validateTimezone(tz); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		ownerID, spec := resolveOwner(ctx, deps, cal.ID, in.Body.OwnerID, evt.OwnerUserID)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		participants, spec := validateEventParticipants(ctx, deps.Queries, cal.ID, in.Body.Participants)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		// Whole-series edit from a composite occurrence ID receives dates for the
		// dragged/edited occurrence. Re-anchor those as a delta against the master
		// so earlier instances do not disappear.
		if isOccurrence && in.Scope == "all" && evt.RecurrenceRule != nil {
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil || !evt.StartAt.Valid {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			originalStart, oerr := occurrenceStartForDate(rule, evt, occurrenceDate)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			duration := endAt.Sub(startAt)
			startAt = evt.StartAt.Time.Add(startAt.Sub(originalStart))
			endAt = startAt.Add(duration)
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, endAt, tz)

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
			overridePubID, err := uuid.NewV7()
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			parentRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
			originalRef := sql.NullTime{Time: originalStart, Valid: true}

			var child generated.CalendarEvent
			err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
				if _, err := q.UpsertRecurrenceOverride(ctx, generated.UpsertRecurrenceOverrideParams{
					PublicID:                overridePubID[:],
					WorkspaceID:             deps.WorkspaceID,
					CalendarID:              cal.ID,
					Kind:                    evt.Kind,
					Visibility:              evt.Visibility,
					ShowAs:                  showAsOrDefault(in.Body.ShowAs),
					Flexibility:             flexibilityOrDefault(in.Body.Flexibility),
					Title:                   in.Body.Title,
					AllDay:                  in.Body.AllDay,
					StartAt:                 sql.NullTime{Time: startAt, Valid: true},
					EndAt:                   sql.NullTime{Time: endAt, Valid: true},
					Timezone:                tz,
					Location:                nullString(in.Body.Location),
					Memo:                    nullString(in.Body.Memo),
					URL:                     nullString(in.Body.URL),
					OwnerUserID:             ownerID,
					CreatedByUserID:         userID,
					NotificationOffset:      ptrIntToNullInt32(in.Body.NotificationOffset),
					RecurrenceParentID:      parentRef,
					RecurrenceOriginalStart: originalRef,
				}); err != nil {
					return err
				}
				var err error
				child, err = q.GetRecurrenceOverride(ctx, generated.GetRecurrenceOverrideParams{
					RecurrenceParentID:      parentRef,
					RecurrenceOriginalStart: originalRef,
				})
				if err != nil {
					return err
				}
				if err := replaceEventParticipants(ctx, q, deps.WorkspaceID, child.ID, participants); err != nil {
					return err
				}
				return eventlog.Append(ctx, q, eventlog.Entry{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  cal.ID,
					ActorUserID: userID,
					Type:        eventlog.TypeEventUpdated,
					Summary:     in.Body.Title,
					Subject:     evt.PublicID,
					Extra: map[string]any{
						"scope":         "occurrence",
						"originalStart": originalStart.Format(time.RFC3339),
					},
				})
			})
			if err != nil {
				slog.ErrorContext(ctx, "failed to write recurrence override", "eventID", evt.ID, "error", err)
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}

			resp := mapOverrideInstance(evt, child, cal.PublicID, originalStart)
			resp.Participants = participantPublicIDList(participants)
			decorate(ctx, deps, &resp, child, cal.ID, nil, nil)
			return &UpdateEventOutput{Body: resp}, nil
		}

		var updated generated.CalendarEvent
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Series-wide edits must keep override rows resolvable: their
			// recurrence_original_start values are keyed to the old occurrence
			// grid, so moving the anchor (e.g. weekly Wed -> Thu) would
			// resurrect cancelled occurrences and duplicate edited ones.
			if evt.RecurrenceRule != nil {
				parentRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
				switch {
				case ruleData == nil:
					// Recurrence removed: the series collapses to a single event
					// and override rows can never resolve to an occurrence again.
					// The exclusion list goes with them, for the same reason.
					if err := q.DeleteRecurrenceOverridesByParent(ctx, parentRef); err != nil {
						return err
					}
					if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
						RecurrenceExceptions: nil,
						ID:                   evt.ID,
					}); err != nil {
						return err
					}
				default:
					if delta := startAt.Sub(evt.StartAt.Time); evt.StartAt.Valid && delta != 0 {
						// The same delta is bound three times because sqlc
						// expands each occurrence of the named argument into
						// its own placeholder rather than reusing one. Binding
						// only the first leaves start_at and end_at at zero
						// microseconds: the override would keep pointing at
						// the right occurrence while showing the old time.
						deltaUs := delta.Microseconds()
						if err := q.ShiftRecurrenceOverrides(ctx, generated.ShiftRecurrenceOverridesParams{
							DeltaUs:            deltaUs,
							DeltaUs_2:          deltaUs,
							DeltaUs_3:          deltaUs,
							RecurrenceParentID: parentRef,
						}); err != nil {
							return err
						}
						// Cancellations are keyed to the same grid, so they have
						// to move with it. Leaving them behind would cancel
						// whichever occurrences now happen to land on the old
						// instants -- usually none, silently resurrecting every
						// occurrence the user had deleted.
						if shifted := shiftExceptions(evt.RecurrenceExceptions, delta); shifted != nil {
							column, err := shifted.MarshalColumn()
							if err != nil {
								return err
							}
							if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
								RecurrenceExceptions: column,
								ID:                   evt.ID,
							}); err != nil {
								return err
							}
						}
					}
				}
			}

			if err := q.UpdateCalendarEvent(ctx, generated.UpdateCalendarEventParams{
				Kind:               evt.Kind,
				Visibility:         evt.Visibility,
				ShowAs:             showAsOrDefault(in.Body.ShowAs),
				Flexibility:        flexibilityOrDefault(in.Body.Flexibility),
				Title:              in.Body.Title,
				AllDay:             in.Body.AllDay,
				StartAt:            sql.NullTime{Time: startAt, Valid: true},
				EndAt:              sql.NullTime{Time: endAt, Valid: true},
				Timezone:           tz,
				Location:           nullString(in.Body.Location),
				Memo:               nullString(in.Body.Memo),
				URL:                nullString(in.Body.URL),
				OwnerUserID:        ownerID,
				BlockLabel:         evt.BlockLabel,
				NotificationOffset: ptrIntToNullInt32(in.Body.NotificationOffset),
				RecurrenceRule:     ruleData,
				RecurrenceEnd:      recEnd,
				ID:                 evt.ID,
			}); err != nil {
				return err
			}
			if err := replaceEventParticipants(ctx, q, deps.WorkspaceID, evt.ID, participants); err != nil {
				return err
			}
			var err error
			updated, err = q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
				WorkspaceID: deps.WorkspaceID,
				PublicID:    evt.PublicID,
			})
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeEventUpdated,
				Summary:     in.Body.Title,
				Subject:     evt.PublicID,
			})
		})
		if err != nil {
			slog.ErrorContext(ctx, "failed to update event", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		resp := mapEvent(updated, cal.PublicID)
		resp.Participants = participantPublicIDList(participants)
		decorate(ctx, deps, &resp, updated, cal.ID, nil, nil)
		return &UpdateEventOutput{Body: resp}, nil
	}
}

// shiftExceptions moves every cancellation by delta, returning nil when
// there is nothing to move.
func shiftExceptions(stored *json.RawMessage, delta time.Duration) recurrence.Exceptions {
	existing := recurrence.ParseExceptions(stored)
	if len(existing) == 0 {
		return nil
	}
	shifted := make(recurrence.Exceptions, 0, len(existing))
	for _, ex := range existing {
		shifted = append(shifted, ex.Add(delta))
	}
	return shifted
}

func DeleteEvent(deps Deps) func(context.Context, *DeleteEventInput) (*DeleteEventOutput, error) {
	return func(ctx context.Context, in *DeleteEventInput) (*DeleteEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// For composite IDs, resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, eventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// Single-occurrence delete: add the occurrence to the series'
		// exclusion list. The contract allows exactly one representation of a
		// cancelled occurrence, and this is it -- a tombstone row would give a
		// reader a second place to look and a way to disagree with the first.
		if isOccurrence && in.Scope == "this" && evt.RecurrenceRule != nil {
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			originalStart, oerr := occurrenceStartForDate(rule, evt, occurrenceDate)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
				// Re-read inside the transaction: the exclusion list is a
				// whole-column replace, so two concurrent cancellations that
				// both read the old value would each write a list missing the
				// other's occurrence.
				current, err := q.GetCalendarEventByID(ctx, evt.ID)
				if err != nil {
					return err
				}
				column, err := recurrence.ParseExceptions(current.RecurrenceExceptions).With(originalStart).MarshalColumn()
				if err != nil {
					return err
				}
				if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
					RecurrenceExceptions: column,
					ID:                   evt.ID,
				}); err != nil {
					return err
				}
				return eventlog.Append(ctx, q, eventlog.Entry{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  cal.ID,
					ActorUserID: userID,
					Type:        eventlog.TypeEventDeleted,
					Summary:     evt.Title,
					Subject:     evt.PublicID,
					Extra: map[string]any{
						"scope":         "occurrence",
						"originalStart": originalStart.Format(time.RFC3339),
					},
				})
			})
			if err != nil {
				slog.ErrorContext(ctx, "failed to cancel occurrence", "eventID", evt.ID, "error", err)
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			return &DeleteEventOutput{}, nil
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// Release the blobs this event's attachments were holding. The
			// objects themselves are swept once nothing refers to them, so
			// the only thing to do here is drop the references.
			objectIDs, err := q.ListAttachmentObjectIDsByEvent(ctx, sql.NullInt32{Int32: int32(evt.ID), Valid: true})
			if err != nil {
				return err
			}
			for _, objectID := range objectIDs {
				if err := q.DecrementStorageObjectRefs(ctx, objectID); err != nil {
					return err
				}
			}
			// Whole-series delete. Soft delete keeps the row, so its override
			// rows keep their parent and the history stays readable.
			if err := q.SoftDeleteCalendarEvent(ctx, evt.ID); err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypeEventDeleted,
				Summary:     evt.Title,
				Subject:     evt.PublicID,
			})
		})
		if err != nil {
			slog.ErrorContext(ctx, "failed to delete event", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteEventOutput{}, nil
	}
}

func ListComments(deps Deps) func(context.Context, *ListCommentsInput) (*ListCommentsOutput, error) {
	return func(ctx context.Context, in *ListCommentsInput) (*ListCommentsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		rows, err := deps.Queries.ListEventComments(ctx, sql.NullInt32{Int32: int32(evt.ID), Valid: true})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListCommentsOutput{Body: make([]CommentResponse, 0, len(rows))}
		for _, c := range rows {
			out.Body = append(out.Body, CommentResponse{
				ID:           pubIDToHex(c.PublicID),
				UserPublicID: pubIDToHex(c.UserPublicID),
				UserName:     c.UserDisplayName,
				UserAvatar:   nullStringValue(c.UserAvatarURL),
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
			return nil, toAPIError(err)
		}

		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if _, err := deps.Queries.CreateEventComment(ctx, generated.CreateEventCommentParams{
			PublicID:    pubID[:],
			WorkspaceID: deps.WorkspaceID,
			EventID:     sql.NullInt32{Int32: int32(evt.ID), Valid: true},
			AuthorID:    userID,
			Body:        in.Body.Content,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Read the row back so createdAt is the stored value rather than a
		// client-side guess at it.
		stored, err := deps.Queries.GetEventCommentByPublicID(ctx, pubID[:])
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &CreateCommentOutput{}
		out.Body = CommentResponse{
			ID:           pubIDToHex(stored.PublicID),
			UserPublicID: pubIDToHex(stored.UserPublicID),
			UserName:     stored.UserDisplayName,
			UserAvatar:   nullStringValue(stored.UserAvatarURL),
			Body:         stored.Body,
			CreatedAt:    stored.CreatedAt,
		}
		return out, nil
	}
}

func resolveCommentEvent(ctx context.Context, deps Deps, calID uint32, eventID string) (generated.CalendarEvent, error) {
	// Comments hang off the series, not an individual occurrence: a
	// composite id resolves to its parent.
	if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
		eventID = parentUUID
	}
	return loadEventInCalendar(ctx, deps, calID, eventID)
}

func UpdateComment(deps Deps) func(context.Context, *UpdateCommentInput) (*UpdateCommentOutput, error) {
	return func(ctx context.Context, in *UpdateCommentInput) (*UpdateCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		commentPub, err := parseUUID(in.CommentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		comment, err := deps.Queries.GetEventCommentByPublicIDAndEvent(ctx, generated.GetEventCommentByPublicIDAndEventParams{
			PublicID: commentPub,
			EventID:  sql.NullInt32{Int32: int32(evt.ID), Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		if comment.AuthorID != userID {
			return nil, apierrors.ToHuma(apierrors.CommentAccessDenied)
		}

		if err := deps.Queries.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
			Body: in.Body.Content,
			ID:   comment.ID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &UpdateCommentOutput{}
		out.Body = CommentResponse{
			ID:           pubIDToHex(comment.PublicID),
			UserPublicID: pubIDToHex(comment.UserPublicID),
			UserName:     comment.UserDisplayName,
			UserAvatar:   nullStringValue(comment.UserAvatarURL),
			Body:         in.Body.Content,
			CreatedAt:    comment.CreatedAt,
		}
		return out, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		evt, err := resolveCommentEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		commentPub, err := parseUUID(in.CommentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		comment, err := deps.Queries.GetEventCommentByPublicIDAndEvent(ctx, generated.GetEventCommentByPublicIDAndEventParams{
			PublicID: commentPub,
			EventID:  sql.NullInt32{Int32: int32(evt.ID), Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.CommentNotFound)
		}
		if comment.AuthorID != userID {
			return nil, apierrors.ToHuma(apierrors.CommentAccessDenied)
		}

		if err := deps.Queries.SoftDeleteEventComment(ctx, comment.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteCommentOutput{}, nil
	}
}
