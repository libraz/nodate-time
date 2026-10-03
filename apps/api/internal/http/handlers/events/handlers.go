package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/daterange"
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

// visibilityOrDefault falls back to the calendar's own setting rather than
// guessing. Reading an unknown value as public would publish something its
// owner marked as not for everyone, which is the one mistake this axis exists
// to prevent.
func visibilityOrDefault(s string) generated.CalendarEventsVisibility {
	switch generated.CalendarEventsVisibility(s) {
	case generated.CalendarEventsVisibilityPublic:
		return generated.CalendarEventsVisibilityPublic
	case generated.CalendarEventsVisibilityPrivate:
		return generated.CalendarEventsVisibilityPrivate
	case generated.CalendarEventsVisibilityConfidential:
		return generated.CalendarEventsVisibilityConfidential
	default:
		return generated.CalendarEventsVisibilityDefault
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
		Visibility:         string(e.Visibility),
		NotificationOffset: nullInt32ToPtr(e.NotificationOffset),
		Participants:       []string{},
		Attendees:          []AttendeeResponse{},
		RecurrenceRule:     mapRecurrenceRule(e.RecurrenceRule),
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          nullTimeValue(e.UpdatedAt),
	}
}

// occurrenceDateKey renders the date half of a recurring instance's composite
// id. The day is read in the event's own timezone, which is the zone the rule
// is anchored in: a series that fires at the same wall-clock time every day
// lands on a different UTC date either side of a DST transition, so two
// neighbouring occurrences would share a key and one of them would be
// impossible to address, edit or cancel.
func occurrenceDateKey(t time.Time, tz string) string {
	return t.In(recurrence.LoadLocation(tz)).Format("20060102")
}

func mapRecurringInstance(e generated.CalendarEvent, calPubID []byte, occ recurrence.Occurrence) EventResponse {
	resp := mapEvent(e, calPubID)
	dateStr := occurrenceDateKey(occ.StartAt, e.Timezone)
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
	// The master's zone, not the override's: the override may have been moved
	// into a different timezone, and the id has to keep naming the occurrence
	// the series produced.
	dateStr := occurrenceDateKey(originalStart, master.Timezone)
	resp.ID = fmt.Sprintf("%s_%s", pubIDToHex(master.PublicID), dateStr)
	// The rule belongs to the series; the override row deliberately carries
	// none of its own, so read it off the master.
	resp.RecurrenceRule = mapRecurrenceRule(master.RecurrenceRule)
	resp.IsRecurrence = true
	resp.RecurrenceDate = &dateStr
	return resp
}

// eventAttendees reads the participant rows once and returns both shapes the
// response carries: the id-only list, which is also the write format, and the
// per-participant state that only a read can tell you.
//
// A failed read is reported rather than rendered as "nobody is attending". The
// participant list is also the write format, so a client that receives an
// empty one and later saves the event sends that emptiness back as the
// authoritative list and every attendee is removed.
func eventAttendees(ctx context.Context, deps Deps, eventID uint32) ([]string, []AttendeeResponse, error) {
	rows, err := deps.Queries.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(eventID), Valid: true})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return []string{}, []AttendeeResponse{}, nil
	}
	ids := make([]string, 0, len(rows))
	attendees := make([]AttendeeResponse, 0, len(rows))
	for _, p := range rows {
		id := pubIDToHex(p.UserPublicID)
		ids = append(ids, id)
		attendees = append(attendees, AttendeeResponse{
			UserID:  id,
			Rsvp:    string(p.Rsvp),
			CanEdit: p.CanEdit,
		})
	}
	return ids, attendees, nil
}

// eventETag names one revision of an event.
//
// It is built from the row's last write rather than a counter column, because
// the shared schema owns the events table and the last-write time is already
// stored at millisecond resolution. Two writes landing in the same millisecond
// would share a tag; they are serialised by the row lock either way, and the
// race this guards against -- two people with the editor open -- is separated
// by seconds, not by fractions of one.
func eventETag(e generated.CalendarEvent) string {
	stamp := e.CreatedAt
	if e.UpdatedAt.Valid {
		stamp = e.UpdatedAt.Time
	}
	return `"` + stamp.UTC().Format("20060102T150405.000") + `"`
}

// matchesETag reports whether a caller's If-Match names the revision on hand.
// An empty header means the caller made no claim, which stays allowed: the
// contract before this existed was last-write-wins, and refusing every
// unconditional update would break callers that never sent one.
func matchesETag(ifMatch string, e generated.CalendarEvent) bool {
	ifMatch = strings.TrimSpace(ifMatch)
	if ifMatch == "" || ifMatch == "*" {
		return true
	}
	current := eventETag(e)
	for _, tag := range strings.Split(ifMatch, ",") {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "W/")
		if tag == current {
			return true
		}
	}
	return false
}

// attendeeSet is every event's participants in a listing, read together.
type attendeeSet map[uint32]struct {
	ids       []string
	attendees []AttendeeResponse
}

// loadAttendees reads the participants of a whole listing in one query.
//
// Asking per event made rendering a month cost one round trip per event on
// it, and a month view fetches every calendar in parallel, so the count
// multiplied by however many calendars the person had.
func loadAttendees(ctx context.Context, deps Deps, eventIDs []uint32) (attendeeSet, error) {
	set := attendeeSet{}
	if len(eventIDs) == 0 {
		return set, nil
	}
	keys := make([]sql.NullInt32, 0, len(eventIDs))
	for _, id := range eventIDs {
		keys = append(keys, sql.NullInt32{Int32: int32(id), Valid: true})
	}
	rows, err := deps.Queries.ListEventAttendeesByEvents(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, p := range rows {
		if !p.EventID.Valid {
			continue
		}
		key := uint32(p.EventID.Int32)
		entry := set[key]
		id := pubIDToHex(p.UserPublicID)
		entry.ids = append(entry.ids, id)
		entry.attendees = append(entry.attendees, AttendeeResponse{
			UserID:  id,
			Rsvp:    string(p.Rsvp),
			CanEdit: p.CanEdit,
		})
		set[key] = entry
	}
	return set, nil
}

// apply fills a response from the batch, which reports an empty list for an
// event with no participants -- a distinction the batch can make because the
// query it came from covered every event asked about.
func (s attendeeSet) apply(resp *EventResponse, eventID uint32) {
	entry, ok := s[eventID]
	if !ok || len(entry.ids) == 0 {
		resp.Participants, resp.Attendees = []string{}, []AttendeeResponse{}
		return
	}
	resp.Participants, resp.Attendees = entry.ids, entry.attendees
}

// setAttendees fills both attendee-derived fields of a response from one read.
func setAttendees(ctx context.Context, deps Deps, resp *EventResponse, eventID uint32) error {
	participants, attendees, err := eventAttendees(ctx, deps, eventID)
	if err != nil {
		return err
	}
	resp.Participants, resp.Attendees = participants, attendees
	return nil
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
	// Only the people leaving are disabled, and only the people arriving are
	// added. Somebody kept keeps their row untouched -- answer and edit grant
	// both -- while a removed row loses its grant, so re-adding a participant
	// cannot hand back an edit right the owner's side did not give again.
	//
	// Only for an event that can already have attendees. Creation calls
	// addEventParticipants directly, and the difference is not tidiness --
	// see there before routing the two back through one function.
	eventRef := sql.NullInt32{Int32: int32(eventID), Valid: true}
	current, err := q.ListEnabledEventAttendeeIDs(ctx, eventRef)
	if err != nil {
		return err
	}
	wanted := make(map[uint32]struct{}, len(participants))
	for _, p := range participants {
		wanted[p.userID] = struct{}{}
	}
	present := make(map[uint32]struct{}, len(current))
	for _, userID := range current {
		present[userID] = struct{}{}
		if _, keep := wanted[userID]; keep {
			continue
		}
		if err := q.DisableEventAttendee(ctx, generated.DisableEventAttendeeParams{EventID: eventRef, UserID: userID}); err != nil {
			return err
		}
	}
	arriving := make([]eventParticipant, 0, len(participants))
	for _, p := range participants {
		if _, ok := present[p.userID]; !ok {
			arriving = append(arriving, p)
		}
	}
	return addEventParticipants(ctx, q, workspaceID, eventID, arriving)
}

// addEventParticipants adds attendees to an event that cannot already have
// any: one created by the same transaction that is now adding them.
//
// The read and disable steps of replaceEventParticipants are deliberately
// absent here, and restoring them to make the two symmetrical would restore an
// intermittent 500 on event creation. An empty UPDATE is not free. Attendee rows are
// indexed on (event_id, user_id) and a brand-new event id sorts above every
// value in that index, so the scan for rows to disable finds nothing and
// still takes a next-key lock running to supremum. Every concurrent creation
// lands on that same gap; gap locks are shared, so all of them get it, and
// then each needs to insert into the gap the others hold. Two creators are
// enough for MySQL to roll one of them back, and the caller sees a request
// that failed for no reason it could act on.
//
// The inserts on their own do not collide: an insert-intention lock conflicts
// with another transaction's gap lock, not with another insert intention.
func addEventParticipants(ctx context.Context, q *generated.Queries, workspaceID, eventID uint32, participants []eventParticipant) error {
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

// supportedNotificationOffsets is the set of reminder offsets, in minutes
// before the start, that a stored event may carry. Anything else is a value no
// client can show or edit: the pickers offer exactly these, and an iCalendar
// import drops an alarm that does not land on one.
var supportedNotificationOffsets = map[int]bool{
	0: true, 5: true, 10: true, 15: true, 30: true,
	60: true, 120: true, 1440: true, 2880: true,
}

// validateNotificationOffset rejects a reminder the product has no way to
// represent. Checking it here rather than only in the picker is what keeps a
// direct API caller from storing an offset that the event modal then silently
// reads back as "no reminder". Returns nil when no reminder is set.
func validateNotificationOffset(offset *int) *apierrors.Spec {
	if offset == nil {
		return nil
	}
	if !supportedNotificationOffsets[*offset] {
		return apierrors.EventNotificationOffsetInvalid
	}
	return nil
}

// resolveOwner maps a public user ID onto the member's internal ID,
// requiring that the user actually belongs to the calendar so an event
// cannot be filed under somebody outside it. A nil owner falls back to
// fallbackUserID, which is the acting user: the contract requires an owner
// on every event, and whoever put it there is the honest answer.
//
// currentOwnerID is the owner the event already has, or 0 for a new event.
// Naming it again is not filing the event under anybody, so it stays
// acceptable even if that user has since left the calendar.
func resolveOwner(ctx context.Context, deps Deps, calID uint32, ownerID *string, fallbackUserID, currentOwnerID uint32) (uint32, *apierrors.Spec) {
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
	if currentOwnerID != 0 && u.ID == currentOwnerID {
		return u.ID, nil
	}
	if _, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: calID,
		UserID:     u.ID,
	}); err != nil {
		return 0, apierrors.BadRequest
	}
	return u.ID, nil
}

// changesGrants reports whether an update reaches past the event's fields:
// handing it to another owner, or dropping an attendee who holds an edit grant,
// which takes that grant away. row is the row the update replaces.
func changesGrants(ctx context.Context, deps Deps, row generated.CalendarEvent, ownerID uint32, participants []eventParticipant) (bool, error) {
	if ownerID != 0 && ownerID != row.OwnerUserID {
		return true, nil
	}
	attendees, err := deps.Queries.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(row.ID), Valid: true})
	if err != nil {
		return false, err
	}
	kept := make(map[uint32]struct{}, len(participants))
	for _, p := range participants {
		kept[p.userID] = struct{}{}
	}
	for _, a := range attendees {
		if _, ok := kept[a.UserID]; a.CanEdit && !ok {
			return true, nil
		}
	}
	return false, nil
}

// userBrief is the public identity of a user (creator/owner) for responses.
type userBrief struct {
	publicID  string
	name      string
	avatarURL string
}

// people is the identity and calendar colour of everyone a response names.
type people struct {
	briefs map[uint32]userBrief
	colors map[uint32]string
}

// loadPeople reads owners and creators in one query however many there are.
// A listing that asked per person cost a round trip per member of a shared
// calendar, on every month change and for every calendar shown at once.
func loadPeople(ctx context.Context, deps Deps, calID uint32, userIDs []uint32) (people, error) {
	p := people{briefs: map[uint32]userBrief{}, colors: map[uint32]string{}}
	if len(userIDs) == 0 {
		return p, nil
	}
	rows, err := deps.Queries.ListEventPeople(ctx, generated.ListEventPeopleParams{
		CalendarID: calID,
		UserIds:    userIDs,
	})
	if err != nil {
		return p, err
	}
	av := avatars.New(deps.Queries, deps.Storage)
	for _, r := range rows {
		if r.Enabled {
			p.briefs[r.ID] = userBrief{
				publicID:  pubIDToHex(r.PublicID),
				name:      r.DisplayName,
				avatarURL: av.FromKey(ctx, r.AvatarStorageKey, r.AvatarURL),
			}
		}
		if r.MemberColor.Valid && r.MemberColor.String != "" {
			p.colors[r.ID] = r.MemberColor.String
		}
	}
	return p, nil
}

// apply fills in the fields that are not columns on the event row: the
// owner's public id, the colour their membership carries, and the creator's
// identity.
func (p people) apply(resp *EventResponse, e generated.CalendarEvent) {
	resp.OwnerID = p.briefs[e.OwnerUserID].publicID
	resp.Color = defaultEventColor
	if c, ok := p.colors[e.OwnerUserID]; ok {
		resp.Color = c
	}
	applyCreator(resp, p.briefs[e.CreatedByUserID])
}

// actorTimezone is the zone the caller reads their calendar in, used when a
// request names dates without saying which zone they are days in.
func actorTimezone(ctx context.Context, deps Deps, userID uint32) string {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.Timezone
}

// applyCreator copies an already-resolved brief onto resp.
func applyCreator(resp *EventResponse, b userBrief) {
	resp.CreatedBy = b.publicID
	resp.CreatorName = b.name
	resp.CreatorAvatarURL = b.avatarURL
}

// decorate is people.apply for a response carrying one event. A failed read
// renders the event without identities and in the default colour rather than
// failing a write that has already happened.
func decorate(ctx context.Context, deps Deps, resp *EventResponse, e generated.CalendarEvent, calID uint32) {
	p, err := loadPeople(ctx, deps, calID, []uint32{e.OwnerUserID, e.CreatedByUserID})
	if err != nil {
		slog.WarnContext(ctx, "failed to read event people", "eventID", e.ID, "error", err)
	}
	p.apply(resp, e)
}

// occurrenceForDate resolves the occurrence falling on dateStr (YYYYMMDD),
// confirming it is a genuine occurrence of the rule rather than an arbitrary
// date crafted into a composite ID. The instants are UTC.
func occurrenceForDate(rule *recurrence.Rule, evt generated.CalendarEvent, dateStr string) (recurrence.Occurrence, error) {
	if !evt.StartAt.Valid || !evt.EndAt.Valid {
		return recurrence.Occurrence{}, fmt.Errorf("event has no dates to expand")
	}
	dayStart, err := occurrenceDayStart(dateStr, evt.Timezone)
	if err != nil {
		return recurrence.Occurrence{}, err
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt.Time, evt.EndAt.Time, dayStart, dayEnd, evt.Timezone) {
		if occurrenceDateKey(occ.StartAt, evt.Timezone) == dateStr {
			return occ, nil
		}
	}
	return recurrence.Occurrence{}, fmt.Errorf("date %s is not an occurrence", dateStr)
}

// occurrenceDayStart turns the date half of a composite id back into the
// instant the day begins in the event's timezone, which is the window the
// occurrence it names can be found in.
func occurrenceDayStart(dateStr, tz string) (time.Time, error) {
	loc := recurrence.LoadLocation(tz)
	day, err := time.ParseInLocation("20060102", dateStr, loc)
	if err != nil {
		return time.Time{}, err
	}
	return day, nil
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

// resolveEventWrite resolves the calendar, the event and the caller's right to
// change that particular event, in one step.
//
// Writing to a calendar and rewriting a given event are different grants. A
// shared calendar carries one layer per person, and an editor who may add to
// their own layer has no business rewriting somebody else's medical
// appointment. Membership decides whether the calendar is writable at all;
// the event's owner, the people running the calendar, and attendees the owner
// explicitly trusted decide the rest.
//
// eventPublicID must already be the series id. occurrenceDate names the
// occurrence a write is confined to, or is empty for a write to the series;
// an edited occurrence's own attendees decide who may change it, because they
// are the attendees that occurrence shows.
func resolveEventWrite(ctx context.Context, deps Deps, calPubID, eventPublicID, occurrenceDate string, userID uint32) (generated.Calendar, generated.CalendarEvent, error) {
	w, err := resolveEventAccess(ctx, deps, calPubID, eventPublicID, occurrenceDate, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	if err := requireEventEdit(ctx, deps, w, userID, editFields); err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	return w.cal, w.evt, nil
}

// eventAccess is everything an edit-right decision reads. row is the row whose
// attendees answer for the write: the series, or an edited occurrence's
// override.
type eventAccess struct {
	cal    generated.Calendar
	member generated.CalendarMember
	evt    generated.CalendarEvent
	row    generated.CalendarEvent
}

// resolveEventAccess loads what requireEventEdit decides on, deciding nothing.
func resolveEventAccess(ctx context.Context, deps Deps, calPubID, eventPublicID, occurrenceDate string, userID uint32) (eventAccess, error) {
	cal, member, err := resolveCalendarMember(ctx, deps, calPubID, userID)
	if err != nil {
		return eventAccess{}, err
	}
	evt, err := loadEventInCalendar(ctx, deps, cal.ID, eventPublicID)
	if err != nil {
		return eventAccess{}, err
	}
	row, err := occurrenceRow(ctx, deps.Queries, evt, occurrenceDate)
	if err != nil {
		return eventAccess{}, err
	}
	return eventAccess{cal: cal, member: member, evt: evt, row: row}, nil
}

// resolveEventForEdit is resolveEventWrite for the paths that take an event id
// straight from the request: checklist items, attachments and the like hang off
// the series, so a composite occurrence id resolves to its parent.
func resolveEventForEdit(ctx context.Context, deps Deps, calPubID, eventID string, userID uint32) (generated.Calendar, generated.CalendarEvent, error) {
	if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
		eventID = parentUUID
	}
	return resolveEventWrite(ctx, deps, calPubID, eventID, "", userID)
}

// occurrenceRow returns the row whose attendees answer for one occurrence of
// evt: the override standing in for it once it has been edited, otherwise evt
// itself. An empty occurrenceDate, or an event without a rule, names evt.
//
// Get and list render an edited occurrence's attendees from its override, so
// every read or write of an occurrence's attendees goes through here to reach
// the same rows.
func occurrenceRow(ctx context.Context, q *generated.Queries, evt generated.CalendarEvent, occurrenceDate string) (generated.CalendarEvent, error) {
	if occurrenceDate == "" || evt.RecurrenceRule == nil {
		return evt, nil
	}
	rule := recurrence.ParseRule(*evt.RecurrenceRule)
	if rule == nil {
		return generated.CalendarEvent{}, apierrors.EventNotFound
	}
	occ, err := occurrenceForDate(rule, evt, occurrenceDate)
	if err != nil {
		return generated.CalendarEvent{}, apierrors.EventNotFound
	}
	child, err := q.GetRecurrenceOverride(ctx, generated.GetRecurrenceOverrideParams{
		RecurrenceParentID:      sql.NullInt32{Int32: int32(evt.ID), Valid: true},
		RecurrenceOriginalStart: sql.NullTime{Time: occ.StartAt, Valid: true},
	})
	switch {
	case err == nil:
		return child, nil
	case errors.Is(err, sql.ErrNoRows):
		return evt, nil
	default:
		return generated.CalendarEvent{}, err
	}
}

// editRight is how far a write reaches into one event.
type editRight int

const (
	// editFields changes what the event says, which an attendee holding an
	// edit grant may do.
	editFields editRight = iota
	// editGrants also changes who owns the event or who may edit it. A
	// delegate that could do this could pass the grant on, or take the event
	// over, and revoking a grant would mean nothing.
	editGrants
)

// requireEventEdit is the one place an event's edit rights are decided. A
// viewer never writes; the people running the calendar and the event's owner
// may do anything; an attendee the owner trusted may change the fields only.
func requireEventEdit(ctx context.Context, deps Deps, w eventAccess, userID uint32, need editRight) error {
	if !calresolve.CanWrite(w.member.Role) {
		return apierrors.CalendarRoleRequired
	}
	if calresolve.CanManage(w.member.Role) || w.evt.OwnerUserID == userID {
		return nil
	}
	if need == editGrants {
		return apierrors.EventEditDenied
	}
	att, err := deps.Queries.GetEventAttendee(ctx, generated.GetEventAttendeeParams{
		EventID: sql.NullInt32{Int32: int32(w.row.ID), Valid: true},
		UserID:  userID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apierrors.EventEditDenied
		}
		return apierrors.InternalUnexpected
	}
	if !att.CanEdit {
		return apierrors.EventEditDenied
	}
	return nil
}

func ListEvents(deps Deps) func(context.Context, *ListEventsInput) (*ListEventsOutput, error) {
	return func(ctx context.Context, in *ListEventsInput) (*ListEventsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		loc := daterange.Location(in.TZ, actorTimezone(ctx, deps, userID))
		window, err := daterange.Resolve(in.StartDate, in.EndDate, in.Days, loc)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		startTime, endTime := window.Start, window.End

		rows, err := deps.Queries.ListCalendarEventsByCalendarAndRange(ctx, generated.ListCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		recurringRows, err := deps.Queries.ListRecurringCalendarEventsByCalendarAndRange(ctx, generated.ListRecurringCalendarEventsByCalendarAndRangeParams{
			CalendarID: cal.ID,
			RangeEnd:   sql.NullTime{Time: endTime, Valid: true},
			RangeStart: sql.NullTime{Time: startTime, Valid: true},
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Everything the listing needs beyond the two range queries is read
		// once per listing. Doing it per event made a month view cost a round
		// trip per event on it, times however many calendars are shown at once.
		seriesIDs := make([]uint32, 0, len(recurringRows))
		for _, e := range recurringRows {
			seriesIDs = append(seriesIDs, e.ID)
		}
		overrides := eventexpand.LoadOverrides(ctx, deps.Queries, seriesIDs)

		// One cap over everything the window holds, cut by date: a listing
		// past it loses the tail of the window, never a whole series or the
		// one-off events read before the series.
		type listed struct {
			series   bool
			row      generated.CalendarEvent
			expanded eventexpand.Instance
		}
		kept := daterange.NewEarliest[listed](daterange.MaxInstances)
		for _, e := range rows {
			kept.Offer(nullTimeValue(e.StartAt), listed{row: e})
		}
		for _, e := range recurringRows {
			for _, expanded := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], startTime, endTime) {
				start := expanded.Occurrence.StartAt
				if expanded.IsOverride {
					start = nullTimeValue(expanded.Event.StartAt)
				}
				kept.Offer(start, listed{series: true, row: e, expanded: expanded})
			}
		}
		items := kept.Items()

		// Attendees and people are read for what is kept, each in one query.
		var eventIDs, userIDs []uint32
		seenEvent, seenUser := map[uint32]bool{}, map[uint32]bool{}
		for _, it := range items {
			e := it.row
			if it.series {
				e = it.expanded.Event
			}
			if !seenEvent[e.ID] {
				seenEvent[e.ID] = true
				eventIDs = append(eventIDs, e.ID)
			}
			for _, id := range []uint32{e.OwnerUserID, e.CreatedByUserID} {
				if !seenUser[id] {
					seenUser[id] = true
					userIDs = append(userIDs, id)
				}
			}
		}
		attendees, err := loadAttendees(ctx, deps, eventIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		who, err := loadPeople(ctx, deps, cal.ID, userIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		results := make([]EventResponse, 0, len(items))
		for _, it := range items {
			switch {
			case !it.series:
				ev := mapEvent(it.row, cal.PublicID)
				attendees.apply(&ev, it.row.ID)
				who.apply(&ev, it.row)
				results = append(results, ev)
			case it.expanded.IsOverride:
				inst := mapOverrideInstance(it.row, it.expanded.Event, cal.PublicID, it.expanded.OriginalStart)
				attendees.apply(&inst, it.expanded.Event.ID)
				who.apply(&inst, it.expanded.Event)
				results = append(results, inst)
			default:
				inst := mapRecurringInstance(it.row, cal.PublicID, it.expanded.Occurrence)
				attendees.apply(&inst, it.row.ID)
				who.apply(&inst, it.row)
				results = append(results, inst)
			}
		}

		out := &ListEventsOutput{Body: results}
		if kept.Truncated() {
			out.Truncated = "true"
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

			// Resolved by the occurrence it names, not by where it now sits:
			// an edited occurrence keeps its id wherever it was moved.
			if evt.RecurrenceRule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			rule := recurrence.ParseRule(*evt.RecurrenceRule)
			if rule == nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			occ, oerr := occurrenceForDate(rule, evt, dateStr)
			if oerr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			expanded, xerr := eventexpand.ResolveOccurrence(ctx, deps.Queries, evt, occ)
			if errors.Is(xerr, eventexpand.ErrCancelled) {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}
			if xerr != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			var resp EventResponse
			source := evt
			if expanded.IsOverride {
				resp = mapOverrideInstance(evt, expanded.Event, cal.PublicID, expanded.OriginalStart)
				source = expanded.Event
			} else {
				resp = mapRecurringInstance(evt, cal.PublicID, expanded.Occurrence)
			}
			if err := setAttendees(ctx, deps, &resp, source.ID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			decorate(ctx, deps, &resp, source, cal.ID)
			// The tag names the series row: an occurrence has no address of
			// its own until it is changed, so one tag has to cover the whole
			// series or a caller would have nothing to hold.
			return &GetEventOutput{ETag: eventETag(evt), Body: resp}, nil
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		resp := mapEvent(evt, cal.PublicID)
		if err := setAttendees(ctx, deps, &resp, evt.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, evt, cal.ID)
		return &GetEventOutput{ETag: eventETag(evt), Body: resp}, nil
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
		if spec := validateNotificationOffset(in.Body.NotificationOffset); spec != nil {
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
		if spec := checkRecurrenceRule(in.Body.RecurrenceRule, startAt, tz); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, endAt, tz)

		ownerID, spec := resolveOwner(ctx, deps, cal.ID, in.Body.OwnerID, userID, 0)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		participants, spec := validateEventParticipants(ctx, deps.Queries, cal.ID, in.Body.Participants)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		var created generated.CalendarEvent
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := calresolve.LockShared(ctx, q, cal.ID); err != nil {
				return err
			}
			result, err := q.CreateCalendarEvent(ctx, generated.CreateCalendarEventParams{
				PublicID:           pubID[:],
				WorkspaceID:        deps.WorkspaceID,
				CalendarID:         cal.ID,
				Kind:               generated.CalendarEventsKindEvent,
				Visibility:         visibilityOrDefault(in.Body.Visibility),
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
			// Added rather than replaced: the row was inserted a statement ago
			// and has no attendees to replace.
			if err := addEventParticipants(ctx, q, deps.WorkspaceID, eventID, participants); err != nil {
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
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to create event", "calendarID", cal.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		resp := mapEvent(created, cal.PublicID)
		if err := setAttendees(ctx, deps, &resp, created.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, created, cal.ID)

		return &CreateEventOutput{ETag: eventETag(created), Body: resp}, nil
	}
}

func UpdateEvent(deps Deps) func(context.Context, *UpdateEventInput) (*UpdateEventOutput, error) {
	return func(ctx context.Context, in *UpdateEventInput) (*UpdateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)

		// For composite IDs (recurring instances), resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		// A write confined to one occurrence is granted by that occurrence's
		// attendees; anything else by the series'.
		grantDate := ""
		if isOccurrence && in.Scope == "this" {
			grantDate = occurrenceDate
		}
		w, err := resolveEventAccess(ctx, deps, in.CalendarID, eventID, grantDate, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if err := requireEventEdit(ctx, deps, w, userID, editFields); err != nil {
			return nil, toAPIError(err)
		}
		cal, evt := w.cal, w.evt
		// A stale copy is reported as stale before its contents are judged:
		// what it names may have changed since, a member who has left included.
		// lockEvent checks again under the lock.
		if !matchesETag(in.IfMatch, evt) {
			return nil, apierrors.ToHuma(apierrors.EventStale)
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
		if spec := validateNotificationOffset(in.Body.NotificationOffset); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		if in.Body.Timezone != "" {
			if spec := validateTimezone(in.Body.Timezone); spec != nil {
				return nil, apierrors.ToHuma(spec)
			}
		}
		ownerID, spec := resolveOwner(ctx, deps, cal.ID, in.Body.OwnerID, 0, w.row.OwnerUserID)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		participants, spec := validateEventParticipants(ctx, deps.Queries, cal.ID, in.Body.Participants)
		if spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		regrants, err := changesGrants(ctx, deps, w.row, ownerID, participants)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if regrants {
			if err := requireEventEdit(ctx, deps, w, userID, editGrants); err != nil {
				return nil, toAPIError(err)
			}
		}

		edit := eventEdit{
			body:         in,
			actorID:      userID,
			cal:          cal,
			startAt:      startAt,
			endAt:        endAt,
			ownerID:      ownerID,
			participants: participants,
		}
		// Both scopes re-read the series under its lock and check If-Match
		// against that copy; the row resolved above only answered whether the
		// caller may edit it.
		var (
			resp EventResponse
			etag string
		)
		switch {
		case isOccurrence && in.Scope == "this" && evt.RecurrenceRule != nil:
			resp, etag, err = editOccurrence(ctx, deps, edit, evt.ID, occurrenceDate)
		case isOccurrence && in.Scope == "all":
			resp, etag, err = editSeries(ctx, deps, edit, evt.ID, occurrenceDate)
		default:
			resp, etag, err = editSeries(ctx, deps, edit, evt.ID, "")
		}
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to update event", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UpdateEventOutput{ETag: etag, Body: resp}, nil
	}
}

func DeleteEvent(deps Deps) func(context.Context, *DeleteEventInput) (*DeleteEventOutput, error) {
	return func(ctx context.Context, in *DeleteEventInput) (*DeleteEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)

		// For composite IDs, resolve the parent series.
		parentUUID, occurrenceDate := parseCompositeID(in.EventID)
		isOccurrence := parentUUID != ""
		eventID := in.EventID
		if isOccurrence {
			eventID = parentUUID
		}

		// A write confined to one occurrence is granted by that occurrence's
		// attendees; anything else by the series'.
		grantDate := ""
		if isOccurrence && in.Scope == "this" {
			grantDate = occurrenceDate
		}
		cal, evt, err := resolveEventWrite(ctx, deps, in.CalendarID, eventID, grantDate, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// Single-occurrence delete: add the occurrence to the series'
		// exclusion list. The contract allows exactly one representation of a
		// cancelled occurrence, and this is it -- a tombstone row would give a
		// reader a second place to look and a way to disagree with the first.
		if isOccurrence && in.Scope == "this" && evt.RecurrenceRule != nil {
			var originalStart time.Time
			err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
				// The exclusion list is a whole-column replace, so it is read
				// under the series' row lock: two cancellations reading the
				// same old list would each write one missing the other's.
				current, err := lockEvent(ctx, q, cal.ID, evt.ID, "")
				if err != nil {
					return err
				}
				if current.RecurrenceRule == nil {
					return apierrors.EventNotFound
				}
				rule := recurrence.ParseRule(*current.RecurrenceRule)
				if rule == nil {
					return apierrors.EventNotFound
				}
				occ, err := occurrenceForDate(rule, current, occurrenceDate)
				if err != nil {
					return apierrors.EventNotFound
				}
				originalStart = occ.StartAt
				column, err := recurrence.ParseExceptions(current.RecurrenceExceptions).With(originalStart).MarshalColumn()
				if err != nil {
					return err
				}
				if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
					RecurrenceExceptions: column,
					ID:                   current.ID,
				}); err != nil {
					return err
				}
				return eventlog.Append(ctx, q, eventlog.Entry{
					WorkspaceID: deps.WorkspaceID,
					CalendarID:  cal.ID,
					ActorUserID: userID,
					Type:        eventlog.TypeEventDeleted,
					Summary:     current.Title,
					Subject:     current.PublicID,
					Extra: map[string]any{
						"scope":         "occurrence",
						"originalStart": originalStart.Format(time.RFC3339),
					},
				})
			})
			if err != nil {
				var spec *apierrors.Spec
				if errors.As(err, &spec) {
					return nil, apierrors.ToHuma(spec)
				}
				slog.ErrorContext(ctx, "failed to cancel occurrence", "eventID", evt.ID, "error", err)
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			return &DeleteEventOutput{}, nil
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// A concurrent delete that won the lock leaves nothing to do here.
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				return err
			}
			// Release the blobs this event's attachments were holding. The
			// objects themselves are swept once nothing refers to them, so
			// the only thing to do here is drop the references.
			//
			// The listing locks the live rows and they are retired in the same
			// transaction, which is what makes the release exactly once: a
			// concurrent delete of the same event, or of one attachment on it,
			// waits on those locks and then finds the rows already retired.
			eventRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
			objectIDs, err := q.ListAttachmentObjectIDsByEvent(ctx, eventRef)
			if err != nil {
				return err
			}
			for _, objectID := range objectIDs {
				if err := blobref.Release(ctx, q, objectID); err != nil {
					return err
				}
			}
			if err := q.SoftDeleteAttachmentsByEvent(ctx, eventRef); err != nil {
				return err
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
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to delete event", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteEventOutput{}, nil
	}
}

// logEventActivity records something that happened to an event -- a comment, a
// checklist item, an attachment -- against the event itself.
//
// The subject is the event's public id rather than the comment's, because the
// history a person reads is the event's: an entry filed under the comment
// would be findable only by somebody who already knew the comment existed.
// What changed is carried alongside it.
func logEventActivity(ctx context.Context, q *generated.Queries, deps Deps, calID, actorID uint32, evt generated.CalendarEvent, eventType, summary string, subjectID []byte) error {
	extra := map[string]any{"event": pubIDToHex(evt.PublicID)}
	if len(subjectID) > 0 {
		extra["subject"] = pubIDToHex(subjectID)
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  calID,
		ActorUserID: actorID,
		Type:        eventType,
		Summary:     summary,
		Subject:     evt.PublicID,
		Extra:       extra,
	})
}

// defaultCommentPageSize and maxCommentPageSize bound one page of a thread.
const (
	defaultCommentPageSize = 50
	maxCommentPageSize     = 200
)

// commentListRow is the shape both comment listings share, so one mapping
// serves the newest page and the pages behind it.
type commentListRow struct {
	id                   uint32
	publicID             []byte
	userPublicID         []byte
	userName             string
	userAvatarURL        sql.NullString
	userAvatarStorageKey sql.NullString
	body                 string
	createdAt            time.Time
}

// resolveCommentCursor turns a cursor back into the ordering pair it names.
// It is looked up within the event the caller has already proved access to,
// so a comment id from elsewhere does not resolve.
func resolveCommentCursor(ctx context.Context, deps Deps, eventID uint32, cursor string) (time.Time, uint32, error) {
	pub, err := parseUUID(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	c, err := deps.Queries.GetEventCommentByPublicIDAndEvent(ctx, generated.GetEventCommentByPublicIDAndEventParams{
		PublicID: pub,
		EventID:  sql.NullInt32{Int32: int32(eventID), Valid: true},
	})
	if err != nil {
		return time.Time{}, 0, err
	}
	return c.CreatedAt, c.ID, nil
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

		limit := int32(in.Limit)
		if limit <= 0 {
			limit = defaultCommentPageSize
		}
		if limit > maxCommentPageSize {
			limit = maxCommentPageSize
		}
		// One extra row is what says whether there is more thread behind this
		// page, without counting the whole of it.
		fetchLimit := limit + 1

		eventID := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
		var rows []commentListRow
		if in.Cursor == "" {
			latest, qerr := deps.Queries.ListEventCommentsLatest(ctx, generated.ListEventCommentsLatestParams{
				WorkspaceID: deps.WorkspaceID,
				EventID:     eventID,
				Limit:       fetchLimit,
			})
			err = qerr
			rows = make([]commentListRow, 0, len(latest))
			for _, c := range latest {
				rows = append(rows, commentListRow{
					id: c.ID, publicID: c.PublicID, userPublicID: c.UserPublicID,
					userName: c.UserDisplayName, userAvatarURL: c.UserAvatarURL,
					userAvatarStorageKey: c.UserAvatarStorageKey,
					body:                 c.Body, createdAt: c.CreatedAt,
				})
			}
		} else {
			before, beforeID, cerr := resolveCommentCursor(ctx, deps, evt.ID, in.Cursor)
			if cerr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			older, qerr := deps.Queries.ListEventCommentsBefore(ctx, generated.ListEventCommentsBeforeParams{
				WorkspaceID:     deps.WorkspaceID,
				EventID:         eventID,
				BeforeCreatedAt: before,
				BeforeID:        beforeID,
				Limit:           fetchLimit,
			})
			err = qerr
			rows = make([]commentListRow, 0, len(older))
			for _, c := range older {
				rows = append(rows, commentListRow{
					id: c.ID, publicID: c.PublicID, userPublicID: c.UserPublicID,
					userName: c.UserDisplayName, userAvatarURL: c.UserAvatarURL,
					userAvatarStorageKey: c.UserAvatarStorageKey,
					body:                 c.Body, createdAt: c.CreatedAt,
				})
			}
		}
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		hasMore := int32(len(rows)) > limit
		if hasMore {
			rows = rows[:limit]
		}
		// The rows arrive newest first, because that is the end of a thread
		// worth bounding. A page is read oldest first, so the cursor comes off
		// the last row before the page is turned round.
		nextCursor := ""
		if hasMore && len(rows) > 0 {
			nextCursor = pubIDToHex(rows[len(rows)-1].publicID)
		}
		slices.Reverse(rows)

		out := &ListCommentsOutput{Body: ListCommentsPage{
			Items:      make([]CommentResponse, 0, len(rows)),
			NextCursor: nextCursor,
		}}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, c := range rows {
			out.Body.Items = append(out.Body.Items, CommentResponse{
				ID:           pubIDToHex(c.publicID),
				UserPublicID: pubIDToHex(c.userPublicID),
				UserName:     c.userName,
				UserAvatar:   av.FromKey(ctx, c.userAvatarStorageKey, c.userAvatarURL),
				Body:         c.body,
				CreatedAt:    c.createdAt,
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
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				return err
			}
			if _, err := q.CreateEventComment(ctx, generated.CreateEventCommentParams{
				PublicID:    pubID[:],
				WorkspaceID: deps.WorkspaceID,
				EventID:     sql.NullInt32{Int32: int32(evt.ID), Valid: true},
				AuthorID:    userID,
				Body:        in.Body.Content,
			}); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeCommentAdded, in.Body.Content, pubID[:])
		}); err != nil {
			return nil, toAPIError(err)
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
			UserAvatar:   avatars.New(deps.Queries, deps.Storage).FromKey(ctx, stored.UserAvatarStorageKey, stored.UserAvatarURL),
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

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				return err
			}
			if err := q.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
				Body: in.Body.Content,
				ID:   comment.ID,
			}); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeCommentEdited, in.Body.Content, comment.PublicID)
		}); err != nil {
			return nil, toAPIError(err)
		}

		out := &UpdateCommentOutput{}
		out.Body = CommentResponse{
			ID:           pubIDToHex(comment.PublicID),
			UserPublicID: pubIDToHex(comment.UserPublicID),
			UserName:     comment.UserDisplayName,
			UserAvatar:   avatars.New(deps.Queries, deps.Storage).FromKey(ctx, comment.UserAvatarStorageKey, comment.UserAvatarURL),
			Body:         in.Body.Content,
			CreatedAt:    comment.CreatedAt,
		}
		return out, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, member, err := resolveCalendarMember(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if !calresolve.CanWrite(member.Role) {
			return nil, apierrors.ToHuma(apierrors.CalendarRoleRequired)
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
		// Removing a comment is moderation as well as authorship: whoever
		// administers a shared calendar has to be able to take down what
		// someone else posted on it. Editing stays the author's alone -- the
		// two are not the same power, since an edit puts words in someone's
		// mouth while a removal only takes them off the wall.
		if comment.AuthorID != userID && !calresolve.CanManage(member.Role) {
			return nil, apierrors.ToHuma(apierrors.CommentAccessDenied)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := lockEvent(ctx, q, cal.ID, evt.ID, ""); err != nil {
				return err
			}
			if err := q.SoftDeleteEventComment(ctx, comment.ID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeCommentRemoved, comment.Body, comment.PublicID)
		}); err != nil {
			return nil, toAPIError(err)
		}
		return &DeleteCommentOutput{}, nil
	}
}
