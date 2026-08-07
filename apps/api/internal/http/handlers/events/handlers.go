package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
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

// occurrenceSeriesETag re-reads a series after one of its occurrences was
// written, so the response hands back the revision that write produced rather
// than the one the request started from. Without the re-read the caller would
// store a tag that is already stale and be refused on its own next save.
func occurrenceSeriesETag(ctx context.Context, deps Deps, seriesID uint32) string {
	master, err := deps.Queries.GetCalendarEventByID(ctx, seriesID)
	if err != nil {
		// Leaving the tag off says "no opinion", which lets the next save
		// through unconditionally -- the behaviour before preconditions
		// existed, and better than handing out one known to be wrong.
		return ""
	}
	return eventETag(master)
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

// actorTimezone is the zone the caller reads their calendar in, used when a
// request names dates without saying which zone they are days in.
func actorTimezone(ctx context.Context, deps Deps, userID uint32) string {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.Timezone
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
	dayStart, err := occurrenceDayStart(dateStr, evt.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt.Time, evt.EndAt.Time, dayStart, dayEnd, evt.Timezone) {
		if occurrenceDateKey(occ.StartAt, evt.Timezone) == dateStr {
			return occ.StartAt.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("date %s is not an occurrence", dateStr)
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
// eventPublicID must already be the series id -- a composite occurrence id is
// split by the caller, because the grant belongs to the series either way.
func resolveEventWrite(ctx context.Context, deps Deps, calPubID, eventPublicID string, userID uint32) (generated.Calendar, generated.CalendarEvent, error) {
	cal, member, err := resolveCalendarMember(ctx, deps, calPubID, userID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	if !calresolve.CanWrite(member.Role) {
		return generated.Calendar{}, generated.CalendarEvent{}, apierrors.CalendarRoleRequired
	}
	evt, err := loadEventInCalendar(ctx, deps, cal.ID, eventPublicID)
	if err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	if err := requireEventEdit(ctx, deps, member, evt, userID); err != nil {
		return generated.Calendar{}, generated.CalendarEvent{}, err
	}
	return cal, evt, nil
}

// resolveEventForEdit is resolveEventWrite for the paths that take an event id
// straight from the request: checklist items, attachments and the like hang off
// the series, so a composite occurrence id resolves to its parent.
func resolveEventForEdit(ctx context.Context, deps Deps, calPubID, eventID string, userID uint32) (generated.Calendar, generated.CalendarEvent, error) {
	if parentUUID, _ := parseCompositeID(eventID); parentUUID != "" {
		eventID = parentUUID
	}
	return resolveEventWrite(ctx, deps, calPubID, eventID, userID)
}

// requireEventEdit is the event-level half of resolveEventWrite, split out for
// the paths that have already loaded the event for other reasons.
func requireEventEdit(ctx context.Context, deps Deps, member generated.CalendarMember, evt generated.CalendarEvent, userID uint32) error {
	if calresolve.CanManage(member.Role) || evt.OwnerUserID == userID {
		return nil
	}
	// A changed occurrence carries its own row; the grant is the series'.
	ownerRef := evt.ID
	if evt.RecurrenceParentID.Valid {
		ownerRef = uint32(evt.RecurrenceParentID.Int32)
	}
	att, err := deps.Queries.GetEventAttendee(ctx, generated.GetEventAttendeeParams{
		EventID: sql.NullInt32{Int32: int32(ownerRef), Valid: true},
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
		window := daterange.Default(in.Days, loc)
		if in.StartDate != "" && in.EndDate != "" {
			window, err = daterange.Parse(in.StartDate, in.EndDate, loc)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
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
		// here, once. Doing it per event made a month view cost a round trip
		// per event on it, times however many calendars are shown at once.
		seriesIDs := make([]uint32, 0, len(recurringRows))
		for _, e := range recurringRows {
			seriesIDs = append(seriesIDs, e.ID)
		}
		overrides := eventexpand.LoadOverrides(ctx, deps.Queries, seriesIDs)

		attendeeIDs := make([]uint32, 0, len(rows)+len(seriesIDs))
		for _, e := range rows {
			attendeeIDs = append(attendeeIDs, e.ID)
		}
		attendeeIDs = append(attendeeIDs, seriesIDs...)
		for _, children := range overrides {
			for _, child := range children {
				attendeeIDs = append(attendeeIDs, child.ID)
			}
		}
		attendees, err := loadAttendees(ctx, deps, attendeeIDs)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		userCache := map[uint32]userBrief{}
		colorCache := map[uint32]string{}
		var results []EventResponse
		for _, e := range rows {
			ev := mapEvent(e, cal.PublicID)
			attendees.apply(&ev, e.ID)
			decorate(ctx, deps, &ev, e, cal.ID, userCache, colorCache)
			results = append(results, ev)
		}

		truncated := false
		for _, e := range recurringRows {
			if len(results) >= daterange.MaxInstances {
				truncated = true
				break
			}
			for _, expanded := range eventexpand.ExpandWithOverrides(e, overrides[e.ID], startTime, endTime) {
				// The window bounds one series; the number of series does not
				// bound itself, and every one of them expands per occurrence.
				if len(results) >= daterange.MaxInstances {
					truncated = true
					break
				}
				if expanded.IsOverride {
					inst := mapOverrideInstance(e, expanded.Event, cal.PublicID, expanded.OriginalStart)
					attendees.apply(&inst, expanded.Event.ID)
					decorate(ctx, deps, &inst, expanded.Event, cal.ID, userCache, colorCache)
					results = append(results, inst)
					continue
				}
				inst := mapRecurringInstance(e, cal.PublicID, expanded.Occurrence)
				attendees.apply(&inst, e.ID)
				decorate(ctx, deps, &inst, e, cal.ID, userCache, colorCache)
				results = append(results, inst)
			}
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].StartAt.Before(results[j].StartAt)
		})

		out := &ListEventsOutput{Body: results}
		if truncated {
			out.Truncated = "true"
		}
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

			dayStart, perr := occurrenceDayStart(dateStr, evt.Timezone)
			if perr != nil {
				return nil, apierrors.ToHuma(apierrors.EventNotFound)
			}

			dayEnd := dayStart.AddDate(0, 0, 1)
			for _, expanded := range eventexpand.ExpandRecurringEvent(ctx, deps.Queries, evt, dayStart, dayEnd) {
				if occurrenceDateKey(expanded.OriginalStart, evt.Timezone) != dateStr {
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
				if err := setAttendees(ctx, deps, &resp, source.ID); err != nil {
					return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
				}
				decorate(ctx, deps, &resp, source, cal.ID, nil, nil)
				// The tag names the series row: an occurrence has no address of
				// its own until it is changed, so one tag has to cover the whole
				// series or a caller would have nothing to hold.
				return &GetEventOutput{ETag: eventETag(evt), Body: resp}, nil
			}
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		evt, err := loadEventInCalendar(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		resp := mapEvent(evt, cal.PublicID)
		if err := setAttendees(ctx, deps, &resp, evt.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, evt, cal.ID, nil, nil)
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
		if err := setAttendees(ctx, deps, &resp, created.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, created, cal.ID, nil, nil)

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

		cal, evt, err := resolveEventWrite(ctx, deps, in.CalendarID, eventID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// The precondition is checked against the row the series is stored in.
		// A per-occurrence edit writes an override that hangs off that row, so
		// both scopes are answering about the same thing changing underneath.
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

		// Omitting visibility keeps what the event already has, the same way
		// timezone works. Treating the omission as "default" would quietly
		// republish an event somebody had marked private, and every edit made
		// by a client that does not know the field would do it.
		visibility := evt.Visibility
		if in.Body.Visibility != "" {
			visibility = visibilityOrDefault(in.Body.Visibility)
		}

		// Same for the two availability axes. A client that does not send them
		// -- a drag, which only means to move the event -- would otherwise
		// reset an out-of-office block to busy and a negotiable meeting to
		// fixed on every move.
		showAs := evt.ShowAs
		if in.Body.ShowAs != "" {
			showAs = showAsOrDefault(in.Body.ShowAs)
		}
		flexibility := evt.Flexibility
		if in.Body.Flexibility != "" {
			flexibility = flexibilityOrDefault(in.Body.Flexibility)
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
					Visibility:              visibility,
					ShowAs:                  showAs,
					Flexibility:             flexibility,
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
				// An occurrence dragged past the end of its own series has to
				// carry that end with it, or the master stops being selected
				// for the window it now falls in and the occurrence vanishes
				// from every view without being deleted.
				if err := q.ExtendRecurrenceEnd(ctx, generated.ExtendRecurrenceEndParams{
					RecurrenceEnd: sql.NullTime{Time: endAt, Valid: true},
					ID:            evt.ID,
					Boundary:      sql.NullTime{Time: endAt, Valid: true},
				}); err != nil {
					return err
				}
				// The occurrence lives in its own row, so without this the
				// series would report the same revision as before the edit and
				// a second writer holding the older copy would be let through.
				if err := q.TouchCalendarEvent(ctx, evt.ID); err != nil {
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
			if err := setAttendees(ctx, deps, &resp, child.ID); err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			decorate(ctx, deps, &resp, child, cal.ID, nil, nil)
			return &UpdateEventOutput{ETag: occurrenceSeriesETag(ctx, deps, evt.ID), Body: resp}, nil
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
					shift := newWallShift(evt.StartAt.Time, startAt, evt.Timezone, tz)
					if evt.StartAt.Valid && !shift.identity() {
						if err := shiftOverrides(ctx, q, parentRef, shift); err != nil {
							return err
						}
						// Cancellations are keyed to the same grid, so they have
						// to move with it. Leaving them behind would cancel
						// whichever occurrences now happen to land on the old
						// instants -- usually none, silently resurrecting every
						// occurrence the user had deleted.
						if shifted := shiftExceptions(evt.RecurrenceExceptions, shift); shifted != nil {
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
				Visibility:         visibility,
				ShowAs:             showAs,
				Flexibility:        flexibility,
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
		if err := setAttendees(ctx, deps, &resp, updated.ID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		decorate(ctx, deps, &resp, updated, cal.ID, nil, nil)
		return &UpdateEventOutput{ETag: eventETag(updated), Body: resp}, nil
	}
}

// shiftExceptions moves every cancellation by delta, returning nil when
// there is nothing to move.
// shiftOverrides moves every override row of a series by the same wall-clock
// shift as the series itself, so each keeps naming the occurrence it replaces
// and keeps the time of day the user gave it.
func shiftOverrides(ctx context.Context, q *generated.Queries, parentRef sql.NullInt32, shift wallShift) error {
	rows, err := q.ListRecurrenceOverridesByParent(ctx, parentRef)
	if err != nil {
		return err
	}
	for _, row := range rows {
		params := generated.RetimeRecurrenceOverrideParams{
			RecurrenceOriginalStart: row.RecurrenceOriginalStart,
			StartAt:                 row.StartAt,
			EndAt:                   row.EndAt,
			ID:                      row.ID,
		}
		if params.RecurrenceOriginalStart.Valid {
			params.RecurrenceOriginalStart.Time = shift.apply(params.RecurrenceOriginalStart.Time)
		}
		if params.StartAt.Valid {
			params.StartAt.Time = shift.apply(params.StartAt.Time)
		}
		if params.EndAt.Valid {
			params.EndAt.Time = shift.apply(params.EndAt.Time)
		}
		if err := q.RetimeRecurrenceOverride(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

func shiftExceptions(stored *json.RawMessage, shift wallShift) recurrence.Exceptions {
	existing := recurrence.ParseExceptions(stored)
	if len(existing) == 0 {
		return nil
	}
	shifted := make(recurrence.Exceptions, 0, len(existing))
	for _, ex := range existing {
		shifted = append(shifted, shift.apply(ex))
	}
	return shifted
}

// wallShift describes how a series-wide edit moved the grid: a number of
// calendar days, a change in the time of day, and the zones the two are read
// and written in.
//
// The unit matters. Occurrences step in calendar units in the event's own
// timezone, so a series whose anchor crosses a DST boundary moves by a
// different number of hours than of days. Shifting a cancellation or an
// override by the absolute difference between the two anchors therefore lands
// it an hour off the grid it is supposed to name: the cancelled occurrence
// comes back and the edited one is orphaned, showing up twice.
type wallShift struct {
	from *time.Location
	to   *time.Location
	days int
	// ofDay is the change in time of day, and may be negative or exceed a day.
	ofDay time.Duration
}

// newWallShift measures the move from oldStart in oldTZ to newStart in newTZ.
func newWallShift(oldStart, newStart time.Time, oldTZ, newTZ string) wallShift {
	from := recurrence.LoadLocation(oldTZ)
	to := recurrence.LoadLocation(newTZ)
	oldLocal := oldStart.In(from)
	newLocal := newStart.In(to)
	oldDay := time.Date(oldLocal.Year(), oldLocal.Month(), oldLocal.Day(), 0, 0, 0, 0, time.UTC)
	newDay := time.Date(newLocal.Year(), newLocal.Month(), newLocal.Day(), 0, 0, 0, 0, time.UTC)
	return wallShift{
		from:  from,
		to:    to,
		days:  int(newDay.Sub(oldDay) / (24 * time.Hour)),
		ofDay: timeOfDay(newLocal) - timeOfDay(oldLocal),
	}
}

// identity reports whether the shift leaves every instant where it is.
func (w wallShift) identity() bool {
	return w.days == 0 && w.ofDay == 0 && w.from == w.to
}

// apply moves one instant by the shift, staying in wall-clock terms
// throughout so a DST boundary crossed on the way does not add an hour.
func (w wallShift) apply(t time.Time) time.Time {
	local := t.In(w.from)
	const day = 24 * time.Hour
	wall := timeOfDay(local) + w.ofDay
	extraDays := int(wall / day)
	wall -= time.Duration(extraDays) * day
	if wall < 0 {
		wall += day
		extraDays--
	}
	target := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, w.days+extraDays)
	return time.Date(
		target.Year(), target.Month(), target.Day(),
		int(wall/time.Hour), int(wall/time.Minute)%60, int(wall/time.Second)%60,
		int(wall%time.Second), w.to,
	).UTC()
}

func timeOfDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour +
		time.Duration(t.Minute())*time.Minute +
		time.Duration(t.Second())*time.Second +
		time.Duration(t.Nanosecond())
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

		cal, evt, err := resolveEventWrite(ctx, deps, in.CalendarID, eventID, userID)
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
			//
			// Retiring the rows in the same transaction is what makes the
			// release exactly once: the listing counts only live rows, so a
			// second delete of the same event finds none and cannot decrement
			// a shared, content-addressed object a live attachment elsewhere
			// still depends on.
			eventRef := sql.NullInt32{Int32: int32(evt.ID), Valid: true}
			objectIDs, err := q.ListAttachmentObjectIDsByEvent(ctx, eventRef)
			if err != nil {
				return err
			}
			for _, objectID := range objectIDs {
				if err := q.DecrementStorageObjectRefs(ctx, objectID); err != nil {
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

		rows, err := deps.Queries.ListEventComments(ctx, generated.ListEventCommentsParams{
			WorkspaceID: deps.WorkspaceID,
			EventID:     sql.NullInt32{Int32: int32(evt.ID), Valid: true},
		})
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
		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
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

		if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdateEventComment(ctx, generated.UpdateEventCommentParams{
				Body: in.Body.Content,
				ID:   comment.ID,
			}); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeCommentEdited, in.Body.Content, comment.PublicID)
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
			if err := q.SoftDeleteEventComment(ctx, comment.ID); err != nil {
				return err
			}
			return logEventActivity(ctx, q, deps, cal.ID, userID, evt,
				eventlog.TypeCommentRemoved, comment.Body, comment.PublicID)
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteCommentOutput{}, nil
	}
}
