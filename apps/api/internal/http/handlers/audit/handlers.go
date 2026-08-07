// Package audit exposes read endpoints over the shared event log: one
// entity's history and a calendar-wide activity feed.
//
// There is no separate history table. Every writer already appends to the
// log in the same transaction as its change, so reading the log is the only
// way to get an account of what happened that cannot disagree with the rows
// it describes.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
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

func toAPIError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
}

// resolveCalendar resolves the calendar by public ID and verifies the caller is
// a member, returning the calendar on success.
func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// resolveActor builds the actor identity from the joined log row fields,
// returning nil for a system action or an actor who no longer exists.
//
// The avatar's storage key rides along on the row and the resolver signs one
// URL per distinct key, so a page of two hundred entries by a handful of
// people costs a handful of signatures and no extra reads.
func resolveActor(ctx context.Context, av *avatars.Resolver, publicID, name, avatarStorageKey, avatarURL sql.NullString) *ActorBrief {
	if !publicID.Valid {
		return nil
	}
	return &ActorBrief{
		ID:        pubIDToHex([]byte(publicID.String)),
		Name:      name.String,
		AvatarURL: av.FromKey(ctx, avatarStorageKey, avatarURL),
	}
}

// payloadFields pulls the summary and subject id back out of a log row.
// Unknown keys are left alone: the contract requires anything that
// round-trips a row to preserve them, and this only reads.
type payloadFields struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
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
// a memo. Matching on the segment rather than the whole string keeps a new
// verb from needing a change here.
func entityTypeOf(eventType string) string {
	parts := strings.Split(eventType, ".")
	if len(parts) >= 3 {
		return parts[1]
	}
	if len(parts) == 2 {
		return parts[0]
	}
	return eventType
}

// EventHistory returns the log entries for a single event.
func EventHistory(deps Deps) func(context.Context, *EventHistoryInput) (*EventHistoryOutput, error) {
	return func(ctx context.Context, in *EventHistoryInput) (*EventHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		// A recurring instance is addressed as "uuid_YYYYMMDD". Every entry
		// for an occurrence -- whether the edit was to "this" instance or to
		// "all" -- is recorded against the parent series' public id, with the
		// occurrence carried in the payload, so the history of any instance
		// is the parent's history. The parent is not looked up in
		// calendar_events: the log is deliberately independent of the live
		// row, so a deleted series' history stays readable.
		eventID := in.EventID
		if parentUUID, _ := calresolve.SplitCompositeID(eventID); parentUUID != "" {
			eventID = parentUUID
		}
		if _, err := uuid.Parse(eventID); err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		items, err := subjectHistory(ctx, deps, cal.ID, eventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &EventHistoryOutput{Body: items}, nil
	}
}

// MemoHistory returns the log entries for a single memo.
func MemoHistory(deps Deps) func(context.Context, *MemoHistoryInput) (*MemoHistoryOutput, error) {
	return func(ctx context.Context, in *MemoHistoryInput) (*MemoHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if _, err := uuid.Parse(in.MemoID); err != nil {
			return nil, apierrors.ToHuma(apierrors.MemoNotFound)
		}

		items, err := subjectHistory(ctx, deps, cal.ID, in.MemoID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &MemoHistoryOutput{Body: items}, nil
	}
}

func subjectHistory(ctx context.Context, deps Deps, calendarID uint32, subjectID string) ([]HistoryItem, error) {
	rows, err := deps.Queries.ListEventsBySubject(ctx, generated.ListEventsBySubjectParams{
		WorkspaceID: deps.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(calendarID), Valid: true},
		SubjectID:   sql.NullString{String: subjectID, Valid: true},
		Limit:       perEntityHistoryLimit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]HistoryItem, 0, len(rows))
	av := avatars.New(deps.Queries, deps.Storage)
	for _, r := range rows {
		payload := readPayload(r.PayloadJSON)
		items = append(items, HistoryItem{
			ID:        pubIDToHex(r.PublicID),
			Action:    r.Type,
			Summary:   payload.Summary,
			CreatedAt: r.OccurredAt,
			Actor:     resolveActor(ctx, av, r.ActorPublicID, r.ActorDisplayName, r.ActorAvatarStorageKey, r.ActorAvatarURL),
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
			return nil, toAPIError(err)
		}

		limit := in.Limit
		if limit <= 0 {
			limit = defaultActivityLimit
		}
		if limit > maxActivityLimit {
			limit = maxActivityLimit
		}
		afterID, err := resolveActivityCursor(ctx, deps, cal.ID, in.Cursor)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		fetchLimit := limit + 1

		rows, err := deps.Queries.ListEventsByCalendar(ctx, generated.ListEventsByCalendarParams{
			WorkspaceID: deps.WorkspaceID,
			CalendarID:  sql.NullInt32{Int32: int32(cal.ID), Valid: true},
			AfterID:     afterID,
			Limit:       int32(fetchLimit),
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		nextCursor := ""
		if len(rows) > limit {
			nextCursor = encodeActivityCursor(rows[limit-1].PublicID)
			rows = rows[:limit]
		}

		out := &ActivityOutput{
			Body: ActivityPage{Items: make([]FeedItem, 0, len(rows)), NextCursor: nextCursor},
		}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, r := range rows {
			payload := readPayload(r.PayloadJSON)
			out.Body.Items = append(out.Body.Items, FeedItem{
				HistoryItem: HistoryItem{
					ID:        pubIDToHex(r.PublicID),
					Action:    r.Type,
					Summary:   payload.Summary,
					CreatedAt: r.OccurredAt,
					Actor:     resolveActor(ctx, av, r.ActorPublicID, r.ActorDisplayName, r.ActorAvatarStorageKey, r.ActorAvatarURL),
				},
				EntityType: entityTypeOf(r.Type),
				EntityID:   payload.ID,
			})
		}
		return out, nil
	}
}
