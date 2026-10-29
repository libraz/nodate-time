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
	rule, err := recurrence.ParseRule(*data)
	if err != nil || rule == nil {
		return nil
	}
	return &RecurrenceRuleResponse{
		Freq:       rule.Freq,
		Interval:   rule.Interval,
		ByDay:      rule.ByDay,
		ByMonthDay: rule.ByMonthDay,
		BySetPos:   rule.BySetPos,
		Wkst:       rule.Wkst,
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

// SupportedNotificationOffset reports whether a reminder offset, in minutes
// before the start, is one the clients can show and edit. Importers use it to
// drop an alarm that does not land on one.
func SupportedNotificationOffset(minutes int) bool {
	return supportedNotificationOffsets[minutes]
}

// validateNotificationOffset rejects a reminder the product has no way to
// represent. Checking it here rather than only in the picker is what keeps a
// direct API caller from storing an offset that the event modal then silently
// reads back as "no reminder". Returns nil when no reminder is set.
func validateNotificationOffset(offset *int) *apierrors.Spec {
	if offset == nil {
		return nil
	}
	if !SupportedNotificationOffset(*offset) {
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
func resolveOwner(ctx context.Context, q *generated.Queries, calID uint32, ownerID *string, fallbackUserID, currentOwnerID uint32) (uint32, *apierrors.Spec) {
	if ownerID == nil || *ownerID == "" {
		return fallbackUserID, nil
	}
	pub, err := parseUUID(*ownerID)
	if err != nil {
		return 0, apierrors.BadRequest
	}
	u, err := q.GetUserByPublicID(ctx, pub)
	if err != nil {
		return 0, apierrors.BadRequest
	}
	if currentOwnerID != 0 && u.ID == currentOwnerID {
		return u.ID, nil
	}
	if _, err := q.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
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
func changesGrants(ctx context.Context, q *generated.Queries, row generated.CalendarEvent, ownerID uint32, participants []eventParticipant) (bool, error) {
	if ownerID != 0 && ownerID != row.OwnerUserID {
		return true, nil
	}
	attendees, err := q.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(row.ID), Valid: true})
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

// eventWriteTarget resolves what a write to an event names: the calendar, the
// series row and, for a composite id, the occurrence date. It refuses a caller
// who may not write to the calendar at all and decides nothing else -- whether
// they may change this event is decided inside the write's transaction, on rows
// read under lock, by lockEventAccess.
func eventWriteTarget(ctx context.Context, deps Deps, calPubID, eventID string, userID uint32) (generated.Calendar, generated.CalendarEvent, string, error) {
	return eventTarget(ctx, deps, calPubID, eventID, userID, resolveCalendarWrite)
}

// eventTarget is eventWriteTarget with the calendar gate the caller names.
func eventTarget(
	ctx context.Context, deps Deps, calPubID, eventID string, userID uint32,
	gate func(context.Context, Deps, string, uint32) (generated.Calendar, error),
) (generated.Calendar, generated.CalendarEvent, string, error) {
	seriesID, occurrenceDate := parseCompositeID(eventID)
	if seriesID == "" {
		seriesID = eventID
	}
	cal, err := gate(ctx, deps, calPubID, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, "", err
	}
	evt, err := loadEventInCalendar(ctx, deps, cal.ID, seriesID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, "", err
	}
	return cal, evt, occurrenceDate, nil
}

// eventAccess is everything an edit-right decision reads, each row read under
// lock in the transaction that writes. row is the row whose attendees answer
// for the write: the series, or an edited occurrence's override when the write
// is confined to that occurrence.
type eventAccess struct {
	cal    generated.Calendar
	member generated.CalendarMember
	evt    generated.CalendarEvent
	row    generated.CalendarEvent
	scope  writeScope
}

// lockEventAccess is the first step of every write to an event: its own
// columns, its exclusion list, or a row hanging off it (an override, an
// attendee, an attachment, a comment, a checklist item). It takes the
// calendar's shared lock, the caller's membership and then the series row
// under their locks, and resolves what occurrenceDate and scope address on
// that row. It decides nothing itself; requireEventEdit decides on what it
// returns.
//
// The order is the point. A row inserted under the event takes a shared lock
// on it for the foreign key, and two writers each holding that while waiting to
// upgrade it deadlock; a delete taking the rows under an event before the event
// deadlocks with a confirm going the other way. Calendar, membership, event,
// then what hangs off it, avoids both, and is the order a member's removal
// takes them in. TestConcurrentFirstEditsOfOccurrencesDoNotDeadlock and
// TestDeletingAnEventWhileAnUploadOnItConfirms are what notice.
func lockEventAccess(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32, occurrenceDate, scope string) (eventAccess, error) {
	member, evt, err := calresolve.LockEventMember(ctx, q, cal.ID, eventID, userID)
	if err != nil {
		return eventAccess{}, err
	}
	target, err := resolveScope(ctx, q, evt, occurrenceDate, scope)
	if err != nil {
		return eventAccess{}, err
	}
	row := evt
	if target.confined && target.override != nil {
		row = *target.override
	}
	return eventAccess{cal: cal, member: member, evt: evt, row: row, scope: target}, nil
}

// lockEventEdit is lockEventAccess for a write to the event as a whole that
// needs the right to change its fields: checklist items and attachments hang
// off the series, so a composite id has already resolved to its parent.
func lockEventEdit(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockEventAccess(ctx, q, cal, eventID, userID, "", scopeAll)
	if err != nil {
		return eventAccess{}, err
	}
	if err := requireEventEdit(ctx, q, w, userID, editFields); err != nil {
		return eventAccess{}, err
	}
	return w, nil
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
// An edited occurrence's own owner -- the one a read of it reports -- counts as
// its owner for a write confined to it, and for nothing beyond it.
//
// w must come from lockEventAccess in q's transaction.
func requireEventEdit(ctx context.Context, q *generated.Queries, w eventAccess, userID uint32, need editRight) error {
	if !calresolve.CanWrite(w.member.Role) {
		return apierrors.CalendarRoleRequired
	}
	if calresolve.CanManage(w.member.Role) || w.evt.OwnerUserID == userID || w.row.OwnerUserID == userID {
		return nil
	}
	if need == editGrants {
		return apierrors.EventEditDenied
	}
	att, err := q.GetEventAttendee(ctx, generated.GetEventAttendeeParams{
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
		overrides, err := eventexpand.LoadOverrides(ctx, deps.Queries, seriesIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

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
			rule, rerr := recurrence.ParseRule(*evt.RecurrenceRule)
			if rerr != nil || rule == nil {
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

		var created generated.CalendarEvent
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			ownerID, spec := resolveOwner(ctx, q, cal.ID, in.Body.OwnerID, userID, 0)
			if spec != nil {
				return spec
			}
			participants, spec := validateEventParticipants(ctx, q, cal.ID, in.Body.Participants)
			if spec != nil {
				return spec
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
				Title:       in.Body.Title,
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
		cal, evt, occurrenceDate, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
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
		if in.Body.Timezone != "" {
			if spec := validateTimezone(in.Body.Timezone); spec != nil {
				return nil, apierrors.ToHuma(spec)
			}
		}

		var edited editedRows
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, occurrenceDate, in.Scope)
			if err != nil {
				return err
			}
			if err := requireEventEdit(ctx, q, w, userID, editFields); err != nil {
				return err
			}
			// A stale copy is reported as stale before its contents are judged:
			// what it names may have changed since, a member who has left included.
			// The tag names the series row, which every scope writes.
			if !matchesETag(in.IfMatch, w.evt) {
				return apierrors.EventStale
			}
			ownerID, spec := resolveOwner(ctx, q, cal.ID, in.Body.OwnerID, 0, w.row.OwnerUserID)
			if spec != nil {
				return spec
			}
			participants, spec := validateEventParticipants(ctx, q, cal.ID, in.Body.Participants)
			if spec != nil {
				return spec
			}
			regrants, err := changesGrants(ctx, q, w.row, ownerID, participants)
			if err != nil {
				return err
			}
			if regrants {
				if err := requireEventEdit(ctx, q, w, userID, editGrants); err != nil {
					return err
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
			if w.scope.confined {
				edited, err = editOccurrence(ctx, q, deps, edit, w)
			} else {
				edited, err = editSeries(ctx, q, deps, edit, w)
			}
			return err
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			slog.ErrorContext(ctx, "failed to update event", "eventID", evt.ID, "error", err)
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		resp, err := edited.render(ctx, deps, cal)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// The tag names the series row, which every edit touches.
		return &UpdateEventOutput{ETag: eventETag(edited.series), Body: resp}, nil
	}
}

func DeleteEvent(deps Deps) func(context.Context, *DeleteEventInput) (*DeleteEventOutput, error) {
	return func(ctx context.Context, in *DeleteEventInput) (*DeleteEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, occurrenceDate, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, occurrenceDate, in.Scope)
			if err != nil {
				return err
			}
			if err := requireEventEdit(ctx, q, w, userID, editFields); err != nil {
				return err
			}
			// A task-projected event goes with its task (PROTOCOL.md
			// obligation 3); deleting it here would desync the pair.
			if w.evt.TaskID.Valid {
				return apierrors.EventTaskOwned
			}
			if w.scope.confined {
				return cancelOccurrence(ctx, q, deps, userID, w)
			}
			return deleteWholeEvent(ctx, q, deps, userID, w)
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

// cancelOccurrence adds one occurrence to the series' exclusion list. The
// contract allows exactly one representation of a cancelled occurrence, and
// this is it -- a tombstone row would give a reader a second place to look and
// a way to disagree with the first. The list is a whole-column replace, which
// is why it is rewritten from the copy read under the series' row lock: two
// cancellations reading the same old list would each write one missing the
// other's.
func cancelOccurrence(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess) error {
	originalStart := w.scope.occurrence.StartAt
	column, err := recurrence.ParseExceptions(w.evt.RecurrenceExceptions).With(originalStart).MarshalColumn()
	if err != nil {
		return err
	}
	if err := q.SetRecurrenceExceptions(ctx, generated.SetRecurrenceExceptionsParams{
		RecurrenceExceptions: column,
		ID:                   w.evt.ID,
	}); err != nil {
		return err
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  w.cal.ID,
		ActorUserID: actorID,
		Type:        eventlog.TypeEventDeleted,
		Title:       w.evt.Title,
		Subject:     w.evt.PublicID,
		Extra: map[string]any{
			"scope":         "occurrence",
			"originalStart": originalStart.Format(time.RFC3339),
		},
	})
}

// deleteWholeEvent retires a one-off event or a whole series with its
// attachments. A retired attachment keeps its reference on the blob until the
// cleanup sweep removes the row, so no count changes here.
func deleteWholeEvent(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess) error {
	eventRef := sql.NullInt32{Int32: int32(w.evt.ID), Valid: true}
	if err := q.SoftDeleteAttachmentsByEvent(ctx, eventRef); err != nil {
		return err
	}
	// Soft delete keeps the row, so its override rows keep their parent and the
	// history stays readable.
	if err := q.SoftDeleteCalendarEvent(ctx, w.evt.ID); err != nil {
		return err
	}
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  w.cal.ID,
		ActorUserID: actorID,
		Type:        eventlog.TypeEventDeleted,
		Title:       w.evt.Title,
		Subject:     w.evt.PublicID,
	})
}

// logEventActivity records something that happened to an event -- a comment, a
// checklist item, an attachment -- against the event itself.
//
// The subject is the event's public id rather than the detail's, because the
// history a person reads is the event's: an entry filed under the comment
// would be findable only by somebody who already knew the comment existed.
// The detail's own id travels in the payload under detailKey (commentId,
// itemId, attachmentId), and summary is its label (item title, file name).
func logEventActivity(ctx context.Context, q *generated.Queries, deps Deps, calID, actorID uint32, evt generated.CalendarEvent, eventType, detailKey, summary string, detailID []byte) error {
	return eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  calID,
		ActorUserID: actorID,
		Type:        eventType,
		Title:       evt.Title,
		Summary:     summary,
		Subject:     evt.PublicID,
		Extra:       map[string]any{detailKey: pubIDToHex(detailID)},
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
// so a comment id from elsewhere does not resolve; a comment removed since the
// cursor was issued still does, because its position in the thread has not
// moved.
func resolveCommentCursor(ctx context.Context, deps Deps, eventID uint32, cursor string) (time.Time, uint32, error) {
	pub, err := parseUUID(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	c, err := deps.Queries.GetEventCommentCursor(ctx, generated.GetEventCommentCursorParams{
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
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		var stored generated.GetEventCommentByPublicIDRow
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			if _, err := q.CreateEventComment(ctx, generated.CreateEventCommentParams{
				PublicID:    pubID[:],
				WorkspaceID: deps.WorkspaceID,
				EventID:     sql.NullInt32{Int32: int32(w.evt.ID), Valid: true},
				AuthorID:    userID,
				Body:        in.Body.Content,
			}); err != nil {
				return err
			}
			if err := logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentCreated, pubID[:]); err != nil {
				return err
			}
			// Read back so createdAt is the stored value rather than a guess at it.
			stored, err = q.GetEventCommentByPublicID(ctx, pubID[:])
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &CreateCommentOutput{Body: mapComment(ctx, deps, stored)}, nil
	}
}

// mapComment renders a stored comment with its author.
func mapComment(ctx context.Context, deps Deps, c generated.GetEventCommentByPublicIDRow) CommentResponse {
	return CommentResponse{
		ID:           pubIDToHex(c.PublicID),
		UserPublicID: pubIDToHex(c.UserPublicID),
		UserName:     c.UserDisplayName,
		UserAvatar:   avatars.New(deps.Queries, deps.Storage).FromKey(ctx, c.UserAvatarStorageKey, c.UserAvatarURL),
		Body:         c.Body,
		CreatedAt:    c.CreatedAt,
	}
}

// lockCommentWrite is lockEventAccess for a comment: anyone who may write to
// the calendar may comment on any event in it, so the role is the whole check.
func lockCommentWrite(ctx context.Context, q *generated.Queries, cal generated.Calendar, eventID, userID uint32) (eventAccess, error) {
	w, err := lockEventAccess(ctx, q, cal, eventID, userID, "", scopeAll)
	if err != nil {
		return eventAccess{}, err
	}
	if !calresolve.CanWrite(w.member.Role) {
		return eventAccess{}, apierrors.CalendarRoleRequired
	}
	return w, nil
}

// lockComment reads one live comment of the event under its row lock.
func lockComment(ctx context.Context, q *generated.Queries, eventID uint32, commentID string) (generated.CalendarEventComment, error) {
	pub, err := parseUUID(commentID)
	if err != nil {
		return generated.CalendarEventComment{}, apierrors.CommentNotFound
	}
	c, err := q.GetEventCommentForUpdate(ctx, generated.GetEventCommentForUpdateParams{
		PublicID: pub,
		EventID:  sql.NullInt32{Int32: int32(eventID), Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.CalendarEventComment{}, apierrors.CommentNotFound
	}
	return c, err
}

// logCommentActivity records a comment being added, edited or removed. The
// entry names the event and the comment but carries none of its text: the log
// is append-only and readable by every member, so text written into it would
// outlive the edit that replaced it and the removal that took it down.
func logCommentActivity(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess, eventType string, commentPubID []byte) error {
	return logEventActivity(ctx, q, deps, w.cal.ID, actorID, w.evt, eventType, "commentId", "", commentPubID)
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
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var stored generated.GetEventCommentByPublicIDRow
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			comment, err := lockComment(ctx, q, w.evt.ID, in.CommentID)
			if err != nil {
				return err
			}
			if comment.AuthorID != userID {
				return apierrors.CommentAccessDenied
			}
			if err := q.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
				Body: in.Body.Content,
				ID:   comment.ID,
			}); err != nil {
				return err
			}
			if err := logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentUpdated, comment.PublicID); err != nil {
				return err
			}
			stored, err = q.GetEventCommentByPublicID(ctx, comment.PublicID)
			return err
		}); err != nil {
			return nil, toAPIError(err)
		}

		return &UpdateCommentOutput{Body: mapComment(ctx, deps, stored)}, nil
	}
}

func DeleteComment(deps Deps) func(context.Context, *DeleteCommentInput) (*DeleteCommentOutput, error) {
	return func(ctx context.Context, in *DeleteCommentInput) (*DeleteCommentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, _, err := eventWriteTarget(ctx, deps, in.CalendarID, in.EventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockCommentWrite(ctx, q, cal, evt.ID, userID)
			if err != nil {
				return err
			}
			comment, err := lockComment(ctx, q, w.evt.ID, in.CommentID)
			if err != nil {
				return err
			}
			// Removing a comment is moderation as well as authorship: whoever
			// administers a shared calendar has to be able to take down what
			// someone else posted on it. Editing stays the author's alone -- the
			// two are not the same power, since an edit puts words in someone's
			// mouth while a removal only takes them off the wall.
			if comment.AuthorID != userID && !calresolve.CanManage(w.member.Role) {
				return apierrors.CommentAccessDenied
			}
			res, err := q.SoftDeleteEventComment(ctx, comment.ID)
			if err != nil {
				return err
			}
			removed, err := blobref.ChangedOne(res)
			if err != nil {
				return err
			}
			if !removed {
				return apierrors.CommentNotFound
			}
			return logCommentActivity(ctx, q, deps, userID, w, eventlog.TypeCommentDeleted, comment.PublicID)
		}); err != nil {
			return nil, toAPIError(err)
		}
		return &DeleteCommentOutput{}, nil
	}
}
