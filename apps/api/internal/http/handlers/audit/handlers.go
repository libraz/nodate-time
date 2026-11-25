// Package audit exposes read endpoints over the shared event log: one
// entity's history and a calendar-wide activity feed.
//
// There is no separate history table. Every writer already appends to the
// log in the same transaction as its change, so reading the log is the only
// way to get an account of what happened that cannot disagree with the rows
// it describes.
package audit

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// Deps holds the dependencies shared by the audit read handlers.
type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	Storage     *storage.Client
	WorkspaceID uint32
}

// defaultActivityLimit and maxActivityLimit bound the activity feed page size.
const (
	defaultActivityLimit  = 50
	maxActivityLimit      = 200
	perEntityHistoryLimit = 200
)

// The cursor names a row by its public id. The feed pages by the internal one
// because that is what orders strictly by insertion, but the internal id is a
// single sequence shared by every calendar in the deployment: a client holding
// two cursors from it could read off how much the whole instance wrote in
// between, which is not something a member of one calendar is entitled to
// know.
func encodeActivityCursor(publicID []byte) string {
	return pubIDToHex(publicID)
}

// resolveActivityCursor turns a cursor back into the row it names. An empty
// cursor means the first page; anything that does not name a row in this
// workspace is a bad request rather than a silent restart from the top.
func resolveActivityCursor(ctx context.Context, deps Deps, calendarID uint32, cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	pub, err := uuid.Parse(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	id, err := deps.Queries.GetEventIDByPublicID(ctx, generated.GetEventIDByPublicIDParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
		PublicID:    pub[:],
	})
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	return id, nil
}

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

// resolveCalendar resolves the calendar by public ID and verifies the caller is
// a member, returning the calendar on success.
func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// resolveActor builds the actor identity from the joined log row fields,
// returning empty strings for a system action or an actor who no longer exists.
//
// The avatar's storage key rides along on the row and the resolver signs one
// URL per distinct key, so a page of two hundred entries by a handful of
// people costs a handful of signatures and no extra reads.
func resolveActor(ctx context.Context, av *avatars.Resolver, publicID, name, avatarStorageKey, avatarURL sql.NullString) (id, actorName, avatarOut string) {
	if !publicID.Valid {
		return "", "", ""
	}
	return pubIDToHex([]byte(publicID.String)), name.String, av.FromKey(ctx, avatarStorageKey, avatarURL)
}

// payloadFields pulls the label and the subject entity's id back out of a log
// row. Unknown keys are left alone: the contract requires anything that
// round-trips a row to preserve them, and this only reads.
type payloadFields struct {
	Summary    string `json:"summary"`
	ItemTitle  string `json:"itemTitle"`
	Filename   string `json:"filename"`
	Title      string `json:"title"`
	EventID    string `json:"eventId"`
	MemoID     string `json:"memoId"`
	PhotoID    string `json:"photoId"`
	InviteID   string `json:"inviteId"`
	LabelID    string `json:"labelId"`
	UserID     string `json:"userId"`
	CalendarID string `json:"calendarId"`
	// OriginalStart names the occurrence of a series the row is about.
	OriginalStart string `json:"originalStart"`
}

// label is the line a feed shows for the row: the summary a writer gave, else
// the name of the detail that changed, else the event's title.
func (f payloadFields) label() string {
	return cmp.Or(f.Summary, f.ItemTitle, f.Filename, f.Title)
}

// entityID is the public id of what the row is about. The registries name it
// per kind (eventId, memoId, ...), and a membership row is about its user.
func (f payloadFields) entityID() string {
	return cmp.Or(f.EventID, f.MemoID, f.PhotoID, f.InviteID, f.LabelID, f.UserID, f.CalendarID)
}

func readPayload(raw json.RawMessage) payloadFields {
	var f payloadFields
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &f)
	}
	return f
}

// entityTypeOf reads the entity out of a dotted event type:
// "calendar.event.created" is about an event, "calendar.memo.deleted" about
// a memo, "calendar.event.comment.created" about a comment. The entity is the
// segment before the verb, so a new verb needs no change here.
func entityTypeOf(eventType string) string {
	parts := strings.Split(eventType, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-2]
	}
	if len(parts) == 2 {
		return parts[0]
	}
	return eventType
}

// EventHistory returns the log entries for a single event, newest first.
func EventHistory(deps Deps) func(context.Context, *EventHistoryInput) (*EventHistoryOutput, error) {
	return func(ctx context.Context, in *EventHistoryInput) (*EventHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		// A recurring instance is addressed as "uuid_YYYYMMDD". Every entry
		// for an occurrence -- whether the edit was to "this" instance or to
		// "all" -- is recorded against the parent series' public id, with the
		// occurrence carried in the payload, so the history of any instance
		// is the parent's history. A series that no longer has a row keeps
		// its history readable.
		eventID := in.EventID
		if parentUUID, _ := calresolve.SplitCompositeID(eventID); parentUUID != "" {
			eventID = parentUUID
		}
		subject, err := uuid.Parse(eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		rd := newReader(cal, userID)
		hidden, err := eventHiddenFrom(ctx, deps, rd, subject[:])
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		if hidden {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		limit := pageLimit(in.Limit)
		before, err := resolveActivityCursor(ctx, deps, cal.ID, in.Cursor)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		shown, more, err := pageOf(ctx, deps, rd, subjectLogSource(deps, cal.ID, subject[:]), beforeBound(before), limit)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		out := &EventHistoryOutput{Body: HistoryPage{Items: make([]HistoryItem, 0, len(shown))}}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, e := range shown {
			out.Body.Items = append(out.Body.Items, historyItem(ctx, av, e))
		}
		if more {
			out.Body.NextCursor = encodeActivityCursor(shown[len(shown)-1].PublicID)
		}
		return out, nil
	}
}

func newReader(cal generated.Calendar, userID uint32) reader {
	return reader{userID: userID, calendarID: cal.ID, calendarDefault: eventacl.Visibility(cal.DefaultEventVisibility)}
}

// eventHiddenFrom reports whether the event has a row in the calendar that
// the reader may not see at all. An event with no row is not hidden: there is
// nothing to protect, and its log rows are dropped one by one.
func eventHiddenFrom(ctx context.Context, deps Deps, rd reader, subject []byte) (bool, error) {
	rows, err := deps.Queries.ListLogSubjectEvents(ctx, generated.ListLogSubjectEventsParams{
		WorkspaceID: deps.WorkspaceID, CalendarID: rd.calendarID, PublicIds: [][]byte{subject},
	})
	if err != nil || len(rows) == 0 {
		return false, err
	}
	probe := logEntry{Type: eventKindPrefix + "updated", SubjectPublicID: subject}
	visible, err := filterVisible(ctx, deps, rd, []logEntry{probe})
	return len(visible) == 0, err
}

func pageLimit(requested int) int {
	switch {
	case requested <= 0:
		return defaultActivityLimit
	case requested > maxActivityLimit:
		return maxActivityLimit
	}
	return requested
}

// beforeBound turns a resolved cursor (0 for the first page) into the
// exclusive upper bound the log queries take.
func beforeBound(resolved uint64) uint64 {
	if resolved == 0 {
		return math.MaxUint64
	}
	return resolved
}

// historyItem renders a log row. A row the reader sees only in part keeps its
// title and loses every other label, since those can carry what was changed.
func historyItem(ctx context.Context, av *avatars.Resolver, e shownEntry) HistoryItem {
	payload := readPayload(e.Payload)
	summary := payload.label()
	if e.masked {
		summary = payload.Title
	}
	actorID, actorName, actorAvatarURL := resolveActor(ctx, av, e.ActorPublicID, e.ActorDisplayName, e.ActorAvatarStorageKey, e.ActorAvatarURL)
	return HistoryItem{
		ID:             pubIDToHex(e.PublicID),
		Action:         e.Type,
		Summary:        summary,
		OriginalStart:  payload.OriginalStart,
		CreatedAt:      e.OccurredAt,
		ActorID:        actorID,
		ActorName:      actorName,
		ActorAvatarURL: actorAvatarURL,
		SystemActor:    e.ActorSystemSource.Valid,
	}
}

// MemoHistory returns the log entries for a single memo.
func MemoHistory(deps Deps) func(context.Context, *MemoHistoryInput) (*MemoHistoryOutput, error) {
	return func(ctx context.Context, in *MemoHistoryInput) (*MemoHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		if _, err := uuid.Parse(in.MemoID); err != nil {
			return nil, apierrors.ToHuma(apierrors.MemoNotFound)
		}

		items, err := subjectHistory(ctx, deps, cal.ID, in.MemoID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &MemoHistoryOutput{Body: items}, nil
	}
}

func subjectHistory(ctx context.Context, deps Deps, calendarID uint32, subjectID string) ([]HistoryItem, error) {
	subject, err := uuid.Parse(subjectID)
	if err != nil {
		return nil, err
	}
	rows, err := deps.Queries.ListEventsBySubject(ctx, generated.ListEventsBySubjectParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
		SubjectID:   subject[:],
		Limit:       perEntityHistoryLimit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]HistoryItem, 0, len(rows))
	av := avatars.New(deps.Queries, deps.Storage)
	for _, r := range rows {
		payload := readPayload(r.PayloadJSON)
		actorID, actorName, actorAvatarURL := resolveActor(ctx, av, r.ActorPublicID, r.ActorDisplayName, r.ActorAvatarStorageKey, r.ActorAvatarURL)
		items = append(items, HistoryItem{
			ID:             pubIDToHex(r.PublicID),
			Action:         r.Type,
			Summary:        payload.label(),
			CreatedAt:      r.OccurredAt,
			ActorID:        actorID,
			ActorName:      actorName,
			ActorAvatarURL: actorAvatarURL,
			SystemActor:    r.ActorSystemSource.Valid,
		})
	}
	return items, nil
}

// Activity returns the calendar-wide activity feed, newest first.
func Activity(deps Deps) func(context.Context, *ActivityInput) (*ActivityOutput, error) {
	return func(ctx context.Context, in *ActivityInput) (*ActivityOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		limit := pageLimit(in.Limit)
		before, err := resolveActivityCursor(ctx, deps, cal.ID, in.Cursor)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		shown, more, err := pageOf(ctx, deps, newReader(cal, userID), calendarLogSource(deps, cal.ID), beforeBound(before), limit)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		out := &ActivityOutput{
			Body: ActivityPage{Items: make([]FeedItem, 0, len(shown))},
		}
		if more {
			out.Body.NextCursor = encodeActivityCursor(shown[len(shown)-1].PublicID)
		}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, e := range shown {
			out.Body.Items = append(out.Body.Items, FeedItem{
				HistoryItem: historyItem(ctx, av, e),
				EntityType:  entityTypeOf(e.Type),
				EntityID:    readPayload(e.Payload).entityID(),
			})
		}
		return out, nil
	}
}
