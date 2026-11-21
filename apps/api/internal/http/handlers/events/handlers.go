package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/libraz/nodate-time/apps/api/internal/eventstore"
	"log/slog"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/attendees"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
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
	resp := EventResponse{
		ID:                 pubIDToHex(e.PublicID),
		CalendarID:         pubIDToHex(calPubID),
		Title:              e.Title,
		Kind:               string(e.Kind),
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
		ParticipantIDs:     []string{},
		Attendees:          []AttendeeResponse{},
		RecurrenceRule:     mapRecurrenceRule(e.RecurrenceRule),
		TaskOwned:          e.TaskID.Valid,
		CreatedAt:          e.CreatedAt,
		UpdatedAt:          nullTimeValue(e.UpdatedAt),
		Etag:               eventETag(e),
	}
	if e.Kind == generated.CalendarEventsKindBlock && e.BlockLabel.Valid {
		resp.BlockLabel = &e.BlockLabel.String
	}
	setDates(&resp)
	return resp
}

const dateLayout = "2006-01-02"

// setDates fills the date pair of an all-day response from its instants, which
// sit on UTC midnights.
func setDates(resp *EventResponse) {
	if !resp.AllDay {
		resp.StartDate, resp.EndDate = nil, nil
		return
	}
	start, end := resp.StartAt.UTC().Format(dateLayout), resp.EndAt.UTC().Format(dateLayout)
	resp.StartDate, resp.EndDate = &start, &end
}

// occurrenceDateKey renders the date half of a recurring instance's composite
// id. The day is read in the zone the series expands in (recurrence.ExpansionZone):
// a series that fires at the same wall-clock time every day lands on a different
// UTC date either side of a DST transition, so two neighbouring occurrences would
// share a key and one of them would be impossible to address, edit or cancel.
func occurrenceDateKey(t time.Time, zone string) string {
	return t.In(recurrence.LoadLocation(zone)).Format("20060102")
}

func mapRecurringInstance(e generated.CalendarEvent, calPubID []byte, occ recurrence.Occurrence) EventResponse {
	resp := mapEvent(e, calPubID)
	dateStr := occurrenceDateKey(occ.StartAt, recurrence.ExpansionZone(e.AllDay, e.Timezone))
	resp.ID = fmt.Sprintf("%s_%s", pubIDToHex(e.PublicID), dateStr)
	resp.StartAt = occ.StartAt
	resp.EndAt = occ.EndAt
	setDates(&resp)
	resp.IsRecurrence = true
	resp.RecurrenceDate = &dateStr
	resp.SeriesEtag = resp.Etag
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
	dateStr := occurrenceDateKey(originalStart, recurrence.ExpansionZone(master.AllDay, master.Timezone))
	resp.ID = fmt.Sprintf("%s_%s", pubIDToHex(master.PublicID), dateStr)
	// The rule belongs to the series; the override row deliberately carries
	// none of its own, so read it off the master.
	resp.RecurrenceRule = mapRecurrenceRule(master.RecurrenceRule)
	resp.IsRecurrence = true
	resp.RecurrenceDate = &dateStr
	resp.SeriesEtag = eventETag(master)
	return resp
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
func changesGrants(ctx context.Context, q *generated.Queries, row generated.CalendarEvent, ownerID uint32, participants []attendees.Person) (bool, error) {
	if ownerID != 0 && ownerID != row.OwnerUserID {
		return true, nil
	}
	current, err := q.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(row.ID), Valid: true})
	if err != nil {
		return false, err
	}
	kept := make(map[uint32]struct{}, len(participants))
	for _, p := range participants {
		kept[p.UserID] = struct{}{}
	}
	for _, a := range current {
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
// owner's identity, the colour their membership carries, and the creator's
// identity.
func (p people) apply(resp *EventResponse, e generated.CalendarEvent) {
	owner := p.briefs[e.OwnerUserID]
	resp.OwnerID = owner.publicID
	resp.OwnerName = owner.name
	resp.OwnerAvatarURL = owner.avatarURL
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
	resp.CreatorID = b.publicID
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
	zone := recurrence.ExpansionZone(evt.AllDay, evt.Timezone)
	dayStart, err := occurrenceDayStart(dateStr, zone)
	if err != nil {
		return recurrence.Occurrence{}, err
	}
	dayEnd := dayStart.AddDate(0, 0, 1)
	for _, occ := range recurrence.ExpandInZone(rule, evt.StartAt.Time, evt.EndAt.Time, dayStart, dayEnd, zone) {
		if occurrenceDateKey(occ.StartAt, zone) == dateStr {
			return occ, nil
		}
	}
	return recurrence.Occurrence{}, fmt.Errorf("date %s is not an occurrence", dateStr)
}

// occurrenceDayStart turns the date half of a composite id back into the
// instant the day begins in the series' expansion zone, which is the window the
// occurrence it names can be found in.
func occurrenceDayStart(dateStr, zone string) (time.Time, error) {
	loc := recurrence.LoadLocation(zone)
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
	// A row with no dates is a placeholder some other product keeps; no
	// surface of this one returns it.
	if err != nil || evt.CalendarID != calID || !evt.StartAt.Valid || !evt.EndAt.Valid {
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

// dateOnly matches the date form of a series bound and of a wire date.
var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Years an all-day date may name. The stored instant must stay representable
// in the shared schema, and a date outside this range is a typo, not a plan.
const (
	minDateYear = 1000
	maxDateYear = 9999
)

// parseDate reads a wire date and checks it names a real day in range.
func parseDate(s string) (time.Time, *apierrors.Spec) {
	d, err := time.Parse(dateLayout, s)
	if err != nil || d.Year() < minDateYear || d.Year() > maxDateYear {
		return time.Time{}, apierrors.EventDateInvalid
	}
	return d, nil
}

// allDayBounds turns an all-day date pair (end exclusive) into the UTC
// midnights that are stored. A milestone is a point, so its end may equal its
// start; every other event ends after it starts.
func allDayBounds(startDate, endDate string, milestone bool) (time.Time, time.Time, *apierrors.Spec) {
	start, spec := parseDate(startDate)
	if spec != nil {
		return time.Time{}, time.Time{}, spec
	}
	end, spec := parseDate(endDate)
	if spec != nil {
		return time.Time{}, time.Time{}, spec
	}
	if end.Before(start) || (end.Equal(start) && !milestone) {
		return time.Time{}, time.Time{}, apierrors.EventDateRangeInvalid
	}
	return start, end, nil
}

// timedBounds parses the instants of a timed event.
func timedBounds(startAt, endAt string) (time.Time, time.Time, *apierrors.Spec) {
	start, err := time.Parse(time.RFC3339, startAt)
	if err != nil {
		return time.Time{}, time.Time{}, apierrors.BadRequest
	}
	end, err := time.Parse(time.RFC3339, endAt)
	if err != nil || end.Before(start) {
		return time.Time{}, time.Time{}, apierrors.BadRequest
	}
	return start, end, nil
}

// checkUntilForm refuses a series bound in the other kind's form: an all-day
// series is bounded by a date, a timed one by an instant.
func checkUntilForm(rule *json.RawMessage, allDay bool) *apierrors.Spec {
	if rule == nil || len(*rule) == 0 || string(*rule) == "null" {
		return nil
	}
	parsed, err := recurrence.ParseRule(*rule)
	if err != nil || parsed == nil || parsed.Until == nil {
		return nil
	}
	if dateOnly.MatchString(*parsed.Until) != allDay {
		return apierrors.EventOccurrenceForm
	}
	return nil
}

func CreateEvent(deps Deps) func(context.Context, *CreateEventInput) (*CreateEventOutput, error) {
	return func(ctx context.Context, in *CreateEventInput) (*CreateEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		var startAt, endAt time.Time
		var spec *apierrors.Spec
		hasInstants := in.Body.StartAt != "" || in.Body.EndAt != ""
		hasDates := in.Body.StartDate != "" || in.Body.EndDate != ""
		if in.Body.AllDay {
			if hasInstants || in.Body.StartDate == "" || in.Body.EndDate == "" {
				return nil, apierrors.ToHuma(apierrors.EventAllDayShape)
			}
			startAt, endAt, spec = allDayBounds(in.Body.StartDate, in.Body.EndDate, false)
		} else {
			if hasDates {
				return nil, apierrors.ToHuma(apierrors.EventAllDayShape)
			}
			startAt, endAt, spec = timedBounds(in.Body.StartAt, in.Body.EndAt)
		}
		if spec != nil {
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
		if spec := checkUntilForm(in.Body.RecurrenceRule, in.Body.AllDay); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}
		zone := recurrence.ExpansionZone(in.Body.AllDay, tz)
		if spec := checkRecurrenceRule(in.Body.RecurrenceRule, startAt, zone); spec != nil {
			return nil, apierrors.ToHuma(spec)
		}

		ruleData, recEnd := prepareRecurrence(in.Body.RecurrenceRule, startAt, endAt, zone)

		var created generated.CalendarEvent
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			ownerID, spec := resolveOwner(ctx, q, cal.ID, in.Body.OwnerID, userID, 0)
			if spec != nil {
				return spec
			}
			participants, spec := validateEventParticipants(ctx, q, cal.ID, in.Body.ParticipantIDs)
			if spec != nil {
				return spec
			}
			attendeeUserIDs := make([]uint32, 0, len(participants))
			for _, participant := range participants {
				attendeeUserIDs = append(attendeeUserIDs, participant.UserID)
			}
			var err error
			created, err = eventstore.Insert(ctx, q, generated.CreateCalendarEventParams{
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
			}, attendeeUserIDs, nil)
			return err
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

		return &CreateEventOutput{ETag: resp.Etag, Body: resp}, nil
	}
}

// touchesHiddenField reports whether a patch names a field a reader with
// hidden details may not write. Naming one is enough: a value equal to the
// stored one would still confirm what it is.
func touchesHiddenField(b *PatchEventInput) bool {
	body := &b.Body
	return body.Location.Present() || body.Memo.Present() || body.URL.Present() ||
		body.Visibility != "" || body.OwnerID != nil || body.ParticipantIDs != nil
}

// checkTag compares the caller's If-Match with the revision the write is
// measured against: the occurrence's own row when the write is confined to an
// edited occurrence, else the series.
func checkTag(ifMatch precondition.IfMatch, w eventAccess) error {
	current := eventETag(w.evt)
	if w.scope.confined && w.scope.override != nil {
		current = eventETag(*w.scope.override)
	}
	switch err := ifMatch.Check(current); {
	case err == nil:
		return nil
	case errors.Is(err, precondition.ErrStale):
		return apierrors.EventStale
	default:
		return err
	}
}

// resolvedPatch is a patch laid over the row it changes.
type resolvedPatch struct {
	edit  eventEdit
	label patch.Field[string]
}

// resolvePatch lays a patch over the stored row and checks everything the
// result must satisfy. w must come from lockEventAccess in q's transaction.
func resolvePatch(ctx context.Context, q *generated.Queries, in *PatchEventInput, w eventAccess, userID uint32) (resolvedPatch, error) {
	body := &in.Body
	base := w.evt
	if w.scope.confined && w.scope.override != nil {
		base = *w.scope.override
	}
	baseStart, baseEnd := base.StartAt.Time, base.EndAt.Time
	if w.scope.occurrence != nil {
		baseStart, baseEnd = w.scope.shown()
	}

	allDay := base.AllDay
	if body.AllDay != nil {
		allDay = *body.AllDay
	}
	switching := allDay != base.AllDay
	hasInstants := body.StartAt != "" || body.EndAt != ""
	hasDates := body.StartDate != "" || body.EndDate != ""

	var startAt, endAt time.Time
	var spec *apierrors.Spec
	if allDay {
		if hasInstants || (switching && (body.StartDate == "" || body.EndDate == "")) {
			return resolvedPatch{}, apierrors.EventAllDayShape
		}
		startDate, endDate := body.StartDate, body.EndDate
		if startDate == "" {
			startDate = baseStart.UTC().Format(dateLayout)
		}
		if endDate == "" {
			endDate = baseEnd.UTC().Format(dateLayout)
		}
		startAt, endAt, spec = allDayBounds(startDate, endDate, base.Kind == generated.CalendarEventsKindMilestone)
	} else {
		if hasDates || (switching && (body.StartAt == "" || body.EndAt == "")) {
			return resolvedPatch{}, apierrors.EventAllDayShape
		}
		startS, endS := body.StartAt, body.EndAt
		if startS == "" {
			startS = baseStart.Format(time.RFC3339)
		}
		if endS == "" {
			endS = baseEnd.Format(time.RFC3339)
		}
		startAt, endAt, spec = timedBounds(startS, endS)
	}
	if spec != nil {
		return resolvedPatch{}, spec
	}

	if body.Timezone != "" {
		if spec := validateTimezone(body.Timezone); spec != nil {
			return resolvedPatch{}, spec
		}
	}
	if body.BlockLabel.Present() && base.Kind != generated.CalendarEventsKindBlock {
		return resolvedPatch{}, apierrors.BadRequest
	}

	var rule *json.RawMessage
	switch raw, ok := body.RecurrenceRule.Get(); {
	case ok:
		rule = &raw
		if spec := checkUntilForm(rule, allDay); spec != nil {
			return resolvedPatch{}, spec
		}
	case body.RecurrenceRule.IsNull():
	case w.evt.TaskID.Valid:
		// A projected event's rule belongs to its task; leaving it out is not
		// a request to change it.
	default:
		rule = w.evt.RecurrenceRule
	}

	ownerID, spec := resolveOwner(ctx, q, w.cal.ID, body.OwnerID, 0, w.row.OwnerUserID)
	if spec != nil {
		return resolvedPatch{}, spec
	}
	participants, err := patchedParticipants(ctx, q, w, body.ParticipantIDs)
	if err != nil {
		return resolvedPatch{}, err
	}

	resolved := &UpdateEventInput{Scope: in.Scope}
	r := &resolved.Body
	r.Title = body.Title
	if r.Title == "" {
		r.Title = base.Title
	}
	r.AllDay = allDay
	r.Timezone = body.Timezone
	r.ShowAs, r.Flexibility, r.Visibility = body.ShowAs, body.Flexibility, body.Visibility
	r.Location = patchedText(body.Location, base.Location)
	r.Memo = patchedText(body.Memo, base.Memo)
	r.URL = patchedText(body.URL, base.URL)
	switch v, ok := body.NotificationOffset.Get(); {
	case ok:
		r.NotificationOffset = &v
	case body.NotificationOffset.IsNull():
	default:
		r.NotificationOffset = nullInt32ToPtr(base.NotificationOffset)
	}
	r.RecurrenceRule = rule

	return resolvedPatch{
		edit: eventEdit{
			body:         resolved,
			actorID:      userID,
			cal:          w.cal,
			startAt:      startAt,
			endAt:        endAt,
			ownerID:      ownerID,
			participants: participants,
		},
		label: body.BlockLabel,
	}, nil
}

// patchedText is a text member after a patch: its value, empty for a null, the
// stored one when left out.
func patchedText(f patch.Field[string], stored sql.NullString) string {
	if v, ok := f.Get(); ok {
		return v
	}
	if f.IsNull() {
		return ""
	}
	return nullStringValue(stored)
}

// patchedParticipants is the participant list after a patch: the one sent, or
// the occurrence's current attendees when none was.
func patchedParticipants(ctx context.Context, q *generated.Queries, w eventAccess, sent []string) ([]attendees.Person, error) {
	if sent != nil {
		people, spec := validateEventParticipants(ctx, q, w.cal.ID, sent)
		if spec != nil {
			return nil, spec
		}
		return people, nil
	}
	source := w.evt.ID
	if w.scope.confined && w.scope.override != nil {
		rows, err := q.ListAttendeeRowsByEvents(ctx, []sql.NullInt32{{Int32: int32(w.scope.override.ID), Valid: true}})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			source = w.scope.override.ID
		}
	}
	current, err := q.ListEventAttendees(ctx, sql.NullInt32{Int32: int32(source), Valid: true})
	if err != nil {
		return nil, err
	}
	people := make([]attendees.Person, 0, len(current))
	for _, a := range current {
		people = append(people, attendees.Person{PublicID: pubIDToHex(a.UserPublicID), UserID: a.UserID, Name: a.DisplayName})
	}
	return people, nil
}

func UpdateEvent(deps Deps) func(context.Context, *PatchEventInput) (*PatchEventOutput, error) {
	return func(ctx context.Context, in *PatchEventInput) (*PatchEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, occurrenceDate, err := eventTarget(ctx, deps, in.CalendarID, in.EventID, userID, resolveCalendar)
		if err != nil {
			return nil, toAPIError(err)
		}

		var edited editedRows
		var seen eventacl.Decision
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, occurrenceDate, in.Scope)
			if err != nil {
				return err
			}
			// What the reader may not see does not exist for them, and what is
			// only masked may not be written through the mask. Both come before
			// the version is compared: a refused field is refused whether the
			// copy is fresh or old.
			d, err := decideForWrite(ctx, q, w, userID)
			if err != nil {
				return err
			}
			if !d.Exists {
				return apierrors.EventNotFound
			}
			seen = d
			if d.DetailsHidden && touchesHiddenField(in) {
				return apierrors.EventDetailsHidden
			}
			need := editFields
			if in.Scope == scopeFollowing {
				need = editGrants
			}
			if err := requireEventEdit(ctx, q, w, userID, need); err != nil {
				return err
			}
			// A stale copy is reported as stale before its contents are judged:
			// what it names may have changed since, a member who has left included.
			if err := checkTag(in.IfMatch, w); err != nil {
				return err
			}
			if in.Scope == scopeFollowing {
				if err := requireRecurring(w); err != nil {
					return err
				}
			}
			resolved, err := resolvePatch(ctx, q, in, w, userID)
			if err != nil {
				return err
			}
			regrants, err := changesGrants(ctx, q, w.row, resolved.edit.ownerID, resolved.edit.participants)
			if err != nil {
				return err
			}
			if regrants {
				if err := requireEventEdit(ctx, q, w, userID, editGrants); err != nil {
					return err
				}
			}
			switch {
			case w.scope.confined:
				edited, err = editOccurrence(ctx, q, deps, resolved.edit, w)
			case w.splitsSeries(in.Scope):
				edited, err = splitSeriesEdit(ctx, q, deps, resolved.edit, w)
			default:
				edited, err = editSeries(ctx, q, deps, resolved.edit, w)
			}
			if err != nil {
				return err
			}
			if resolved.label.Present() {
				edited, err = setBlockLabel(ctx, q, edited, resolved.label)
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
		withhold(&resp, seen)
		return &PatchEventOutput{ETag: resp.Etag, Body: resp}, nil
	}
}

// setBlockLabel writes a block's label on the row the edit produced and
// re-reads the rows the response is rendered from.
func setBlockLabel(ctx context.Context, q *generated.Queries, edited editedRows, label patch.Field[string]) (editedRows, error) {
	target := &edited.series
	if edited.override != nil {
		target = edited.override
	}
	value, _ := label.Get()
	if err := q.SetCalendarEventBlockLabel(ctx, generated.SetCalendarEventBlockLabelParams{
		BlockLabel: nullString(value), ID: target.ID,
	}); err != nil {
		return edited, err
	}
	series, err := q.GetCalendarEventByID(ctx, edited.series.ID)
	if err != nil {
		return edited, err
	}
	edited.series = series
	if edited.override != nil {
		override, err := q.GetCalendarEventByID(ctx, edited.override.ID)
		if err != nil {
			return edited, err
		}
		edited.override = &override
	}
	return edited, nil
}

func DeleteEvent(deps Deps) func(context.Context, *DeleteEventInput) (*DeleteEventOutput, error) {
	return func(ctx context.Context, in *DeleteEventInput) (*DeleteEventOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, evt, occurrenceDate, err := eventTarget(ctx, deps, in.CalendarID, in.EventID, userID, resolveCalendar)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			w, err := lockEventAccess(ctx, q, cal, evt.ID, userID, occurrenceDate, in.Scope)
			if err != nil {
				return err
			}
			d, err := decideForWrite(ctx, q, w, userID)
			if err != nil {
				return err
			}
			if !d.Exists {
				return apierrors.EventNotFound
			}
			// Removing a whole series takes every occurrence with it, which an
			// attendee holding an edit grant may not do to the owner's plans.
			need := editFields
			if in.Scope == scopeFollowing || (!w.scope.confined && w.evt.RecurrenceRule != nil) {
				need = editGrants
			}
			if err := requireEventEdit(ctx, q, w, userID, need); err != nil {
				return err
			}
			if err := checkTag(in.IfMatch, w); err != nil {
				return err
			}
			// A task-projected event goes with its task (PROTOCOL.md
			// obligation 3); deleting it here would desync the pair.
			if w.evt.TaskID.Valid {
				return apierrors.EventTaskOwned
			}
			if in.Scope == scopeFollowing {
				if err := requireRecurring(w); err != nil {
					return err
				}
			}
			switch {
			case w.scope.confined:
				return cancelOccurrence(ctx, q, deps, userID, w)
			case w.splitsSeries(in.Scope):
				return splitSeriesDelete(ctx, q, deps, userID, w)
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
	column, err := recurrence.ParseExceptionList(w.evt.RecurrenceExceptions).
		With(originalStart, w.evt.AllDay, w.evt.Timezone).MarshalColumn()
	if err != nil {
		return err
	}
	if err := q.SetCancelledOccurrences(ctx, generated.SetCancelledOccurrencesParams{
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
// cleanup sweep removes the row, so no count changes here. The attachments are
// named in the log row, so a restore brings back exactly the ones this
// deletion took.
func deleteWholeEvent(ctx context.Context, q *generated.Queries, deps Deps, actorID uint32, w eventAccess) error {
	live, err := q.ListLiveAttachmentPublicIDsByEvent(ctx, sql.NullInt32{Int32: int32(w.evt.ID), Valid: true})
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(live))
	for _, id := range live {
		ids = append(ids, pubIDToHex(id))
	}
	return eventstore.SoftDelete(ctx, q, deps.WorkspaceID, w.cal.ID, actorID, w.evt, map[string]any{"attachmentIds": ids})
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
