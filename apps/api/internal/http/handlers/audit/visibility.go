package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
)

// logBatchSize is how many raw log rows are read per round when hidden rows
// have to be skipped to fill a page.
const logBatchSize = 200

// eventKindPrefix marks the kinds whose subject is a calendar event. Only
// those pass through the visibility rules; member, memo, album and invite
// rows do not.
const eventKindPrefix = "calendar.event."

// logEntry is one log row with its actor joined in, whichever query read it.
type logEntry struct {
	ID                    uint64
	PublicID              []byte
	SubjectPublicID       []byte
	Type                  string
	Payload               json.RawMessage
	OccurredAt            time.Time
	ActorSystemSource     sql.NullString
	ActorPublicID         sql.NullString
	ActorDisplayName      sql.NullString
	ActorAvatarURL        sql.NullString
	ActorAvatarStorageKey sql.NullString
}

// logSource reads up to limit rows older than beforeID, newest first.
type logSource func(ctx context.Context, beforeID uint64, limit int32) ([]logEntry, error)

// shownEntry is a row that passed the visibility rules.
type shownEntry struct {
	logEntry
	// masked is true when the reader is a member without access to the
	// event's details: only the title may be shown.
	masked bool
}

func calendarLogSource(deps Deps, calendarID uint32) logSource {
	return func(ctx context.Context, beforeID uint64, limit int32) ([]logEntry, error) {
		rows, err := deps.Queries.ListCalendarLogPage(ctx, generated.ListCalendarLogPageParams{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
			BeforeID:    beforeID,
			Limit:       limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]logEntry, len(rows))
		for i, r := range rows {
			out[i] = logEntry{r.ID, r.PublicID, r.SubjectPublicID, r.Type, r.PayloadJSON, r.OccurredAt,
				r.ActorSystemSource, r.ActorPublicID, r.ActorDisplayName, r.ActorAvatarURL, r.ActorAvatarStorageKey}
		}
		return out, nil
	}
}

func subjectLogSource(deps Deps, calendarID uint32, subject []byte) logSource {
	return func(ctx context.Context, beforeID uint64, limit int32) ([]logEntry, error) {
		rows, err := deps.Queries.ListSubjectLogPage(ctx, generated.ListSubjectLogPageParams{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
			SubjectID:   subject,
			BeforeID:    beforeID,
			Limit:       limit,
		})
		if err != nil {
			return nil, err
		}
		out := make([]logEntry, len(rows))
		for i, r := range rows {
			out[i] = logEntry{r.ID, r.PublicID, r.SubjectPublicID, r.Type, r.PayloadJSON, r.OccurredAt,
				r.ActorSystemSource, r.ActorPublicID, r.ActorDisplayName, r.ActorAvatarURL, r.ActorAvatarStorageKey}
		}
		return out, nil
	}
}

// reader is the viewing member and the calendar they are reading.
type reader struct {
	userID          uint32
	calendarID      uint32
	calendarDefault eventacl.Visibility
}

// pageOf returns up to limit visible rows older than beforeID, plus whether
// more visible rows follow. Rows the reader may not see do not count toward
// the page, so a page is short only at the end of the log.
func pageOf(ctx context.Context, deps Deps, rd reader, src logSource, beforeID uint64, limit int) ([]shownEntry, bool, error) {
	var shown []shownEntry
	for len(shown) <= limit {
		rows, err := src(ctx, beforeID, logBatchSize)
		if err != nil {
			return nil, false, err
		}
		batch, err := filterVisible(ctx, deps, rd, rows)
		if err != nil {
			return nil, false, err
		}
		shown = append(shown, batch...)
		if len(rows) < logBatchSize {
			break
		}
		beforeID = rows[len(rows)-1].ID
	}
	if len(shown) > limit {
		return shown[:limit], true, nil
	}
	return shown, false, nil
}

// occurrenceFacts is what the decision needs about one occurrence: the row
// that stands for it and the attendee rows of the series and of its override.
type occurrenceFacts struct {
	event    eventacl.Event
	series   []eventacl.Attendee
	override []eventacl.Attendee
}

// filterVisible drops the rows the reader may not see and flags the ones
// they may see only in part. It reads the subjects, overrides and attendees
// for the whole batch in one round each.
func filterVisible(ctx context.Context, deps Deps, rd reader, rows []logEntry) ([]shownEntry, error) {
	type subjectRow = generated.ListLogSubjectEventsRow

	var subjectIDs [][]byte
	seen := map[string]bool{}
	for _, r := range rows {
		if !isEventRow(r) || seen[string(r.SubjectPublicID)] {
			continue
		}
		seen[string(r.SubjectPublicID)] = true
		subjectIDs = append(subjectIDs, r.SubjectPublicID)
	}

	subjects := map[string]subjectRow{}
	if len(subjectIDs) > 0 {
		found, err := deps.Queries.ListLogSubjectEvents(ctx, generated.ListLogSubjectEventsParams{
			WorkspaceID: deps.WorkspaceID, CalendarID: rd.calendarID, PublicIds: subjectIDs,
		})
		if err != nil {
			return nil, err
		}
		for _, s := range found {
			subjects[string(s.PublicID)] = s
		}
	}

	// Only rows that name an occurrence need the overrides.
	var parentIDs []sql.NullInt32
	parentSeen := map[uint32]bool{}
	for _, r := range rows {
		if !isEventRow(r) || readPayload(r.Payload).OriginalStart == "" {
			continue
		}
		if s, ok := subjects[string(r.SubjectPublicID)]; ok && !parentSeen[s.ID] {
			parentSeen[s.ID] = true
			parentIDs = append(parentIDs, sql.NullInt32{Int32: int32(s.ID), Valid: true})
		}
	}
	overrides := map[uint32][]generated.ListLogOverridesRow{}
	if len(parentIDs) > 0 {
		found, err := deps.Queries.ListLogOverrides(ctx, parentIDs)
		if err != nil {
			return nil, err
		}
		for _, o := range found {
			overrides[uint32(o.RecurrenceParentID.Int32)] = append(overrides[uint32(o.RecurrenceParentID.Int32)], o)
		}
	}

	eventIDs := make([]sql.NullInt32, 0, len(subjects))
	for _, s := range subjects {
		eventIDs = append(eventIDs, sql.NullInt32{Int32: int32(s.ID), Valid: true})
	}
	for _, list := range overrides {
		for _, o := range list {
			eventIDs = append(eventIDs, sql.NullInt32{Int32: int32(o.ID), Valid: true})
		}
	}
	attendees := map[uint32][]eventacl.Attendee{}
	if len(eventIDs) > 0 {
		found, err := deps.Queries.ListLogAttendees(ctx, eventIDs)
		if err != nil {
			return nil, err
		}
		for _, a := range found {
			id := uint32(a.EventID.Int32)
			attendees[id] = append(attendees[id], eventacl.Attendee{UserID: a.UserID, Enabled: a.Enabled})
		}
	}

	facts := func(r logEntry) (occurrenceFacts, bool) {
		s, ok := subjects[string(r.SubjectPublicID)]
		if !ok {
			return occurrenceFacts{}, false
		}
		f := occurrenceFacts{
			event:  eventacl.Event{Visibility: eventacl.Visibility(s.Visibility), OwnerUserID: s.OwnerUserID, CreatedByUserID: s.CreatedByUserID},
			series: attendees[s.ID],
		}
		if start, ok := parseOriginalStart(readPayload(r.Payload).OriginalStart); ok {
			for _, o := range overrides[s.ID] {
				if o.RecurrenceOriginalStart.Valid && o.RecurrenceOriginalStart.Time.Equal(start) {
					f.event = eventacl.Event{Visibility: eventacl.Visibility(o.Visibility), OwnerUserID: o.OwnerUserID, CreatedByUserID: o.CreatedByUserID}
					f.override = attendees[o.ID]
					break
				}
			}
		}
		f.event.CalendarDefault = rd.calendarDefault
		return f, true
	}

	out := make([]shownEntry, 0, len(rows))
	for _, r := range rows {
		if !isEventRow(r) {
			out = append(out, shownEntry{logEntry: r})
			continue
		}
		f, ok := facts(r)
		if !ok {
			continue
		}
		ids := eventacl.OccurrenceAttendees(f.series, f.override)
		d := eventacl.Decide(f.event, eventacl.Reader{UserID: rd.userID, IsMember: true}, ids)
		if !d.Exists {
			continue
		}
		out = append(out, shownEntry{logEntry: r, masked: d.DetailsHidden})
	}
	return out, nil
}

func isEventRow(r logEntry) bool {
	return len(r.SubjectPublicID) == 16 && strings.HasPrefix(r.Type, eventKindPrefix)
}

// parseOriginalStart reads the occurrence start a writer recorded, as an
// instant or as a bare date.
func parseOriginalStart(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}
