package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
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
		TaskOwned:          e.TaskID.Valid,
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

// InsertEvent persists a validated new event, its attendees, and history using
// the caller's transaction. Callers resolve the calendar grant and attendee
// membership before calling it.
func InsertEvent(ctx context.Context, q *generated.Queries, params generated.CreateCalendarEventParams, attendeeUserIDs []uint32) (generated.CalendarEvent, error) {
	result, err := q.CreateCalendarEvent(ctx, params)
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	if err := addEventParticipants(ctx, q, params.WorkspaceID, uint32(eventID), attendeeUserIDs); err != nil {
		return generated.CalendarEvent{}, err
	}
	created, err := q.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: params.WorkspaceID,
		PublicID:    params.PublicID,
	})
	if err != nil {
		return generated.CalendarEvent{}, err
	}
	if err := eventlog.Append(ctx, q, eventlog.Entry{
		WorkspaceID: params.WorkspaceID,
		CalendarID:  params.CalendarID,
		ActorUserID: params.CreatedByUserID,
		Type:        eventlog.TypeEventCreated,
		Title:       params.Title,
		Subject:     params.PublicID,
	}); err != nil {
		return generated.CalendarEvent{}, err
	}
	return created, nil
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
			attendeeUserIDs := make([]uint32, 0, len(participants))
			for _, participant := range participants {
				attendeeUserIDs = append(attendeeUserIDs, participant.userID)
			}
			var err error
			created, err = InsertEvent(ctx, q, generated.CreateCalendarEventParams{
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
			}, attendeeUserIDs)
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
