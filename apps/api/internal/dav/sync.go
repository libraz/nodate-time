package dav

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-webdav/caldav"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/ical"
	"github.com/libraz/nodate-time/apps/api/internal/icsexport"
)

// A sync token is the calendar's latest log row when it was issued, the UTC
// day it was issued on and the renderer version that drew what it covers. A
// token from another day is refused, so a client resynchronises fully at
// least daily: that is what removes what the log cannot report (a series that
// left the reader's view, a row purged outright).
const tokenPrefix = "urn:nodate-time:sync:"

const (
	// maxSyncRows is how many new log rows a token may be behind before a full
	// resynchronisation is cheaper than replaying them.
	maxSyncRows = 5000
	// rereadWindow reaches back past the token's row for rows a transaction
	// that started earlier committed later, with an id below it.
	rereadWindow = 5 * time.Minute
	syncPageSize = 1000
)

// logAction is what a log row means to a synchronising client.
type logAction int

const (
	// actionIgnore: nothing a calendar object renders changed.
	actionIgnore logAction = iota
	// actionRender: the subject series may have changed.
	actionRender
	// actionResync: something every object depends on changed.
	actionResync
	// actionResyncIfReader: a role change, which matters only for the reader's own.
	actionResyncIfReader
)

// logActions holds every kind in sql/core/event-kinds.yaml and
// sql/time/event-kinds.yaml; a kind missing here is ignored.
var logActions = map[string]logAction{
	eventlog.TypeEventCreated:    actionRender,
	eventlog.TypeEventUpdated:    actionRender,
	eventlog.TypeEventDeleted:    actionRender,
	eventlog.TypeEventRestored:   actionRender,
	eventlog.TypeAttendeeAdded:   actionRender,
	eventlog.TypeAttendeeRemoved: actionRender,
	eventlog.TypeAttendeeUpdated: actionRender,
	eventlog.TypeRSVPUpdated:     actionRender,

	eventlog.TypeCalendarEdited:         actionResync,
	eventlog.TypeCalendarGone:           actionResync,
	eventlog.TypeMemberRoleSet:          actionResyncIfReader,
	eventlog.TypeWorkspaceMemberRemoved: actionResync,

	eventlog.TypeCalendarSetUp:               actionIgnore,
	eventlog.TypeCalendarSubscribed:          actionIgnore,
	eventlog.TypeCalendarSubscriptionUpdated: actionIgnore,
	eventlog.TypeMemberAdded:                 actionIgnore,
	eventlog.TypeMemberRemoved:               actionIgnore,
	eventlog.TypeMemberColorSet:              actionIgnore,
	eventlog.TypeCommentCreated:              actionIgnore,
	eventlog.TypeCommentUpdated:              actionIgnore,
	eventlog.TypeCommentDeleted:              actionIgnore,
	eventlog.TypeChecklistCreated:            actionIgnore,
	eventlog.TypeChecklistUpdated:            actionIgnore,
	eventlog.TypeChecklistDeleted:            actionIgnore,
	eventlog.TypeAttachmentCreated:           actionIgnore,
	eventlog.TypeAttachmentDeleted:           actionIgnore,
	eventlog.TypeEventInviteCreated:          actionIgnore,
	eventlog.TypeEventInviteRotated:          actionIgnore,
	eventlog.TypeEventInviteRevoked:          actionIgnore,
	eventlog.TypeMemoCreated:                 actionIgnore,
	eventlog.TypeMemoUpdated:                 actionIgnore,
	eventlog.TypeMemoCompleted:               actionIgnore,
	eventlog.TypeMemoDeleted:                 actionIgnore,
	eventlog.TypeReminder:                    actionIgnore,
	eventlog.TypeInviteCreated:               actionIgnore,
	eventlog.TypeInviteRevoked:               actionIgnore,
	eventlog.TypePhotoUploaded:               actionIgnore,
	eventlog.TypePhotoUpdated:                actionIgnore,
	eventlog.TypePhotoDeleted:                actionIgnore,
	eventlog.TypeLabelCreated:                actionIgnore,
	eventlog.TypeLabelUpdated:                actionIgnore,
	eventlog.TypeLabelDeleted:                actionIgnore,
	eventlog.TypeWorkspaceMemberAdded:        actionIgnore,
	eventlog.TypeWorkspaceMemberRoleChanged:  actionIgnore,
}

type syncToken struct {
	calendar string
	logID    uint64
	day      int64
	version  string
}

func (t syncToken) String() string {
	return fmt.Sprintf("%s%s:%d:%d:%s", tokenPrefix, t.calendar, t.logID, t.day, t.version)
}

func parseToken(s string) (syncToken, bool) {
	if !strings.HasPrefix(s, tokenPrefix) {
		return syncToken{}, false
	}
	parts := strings.Split(strings.TrimPrefix(s, tokenPrefix), ":")
	if len(parts) != 4 {
		return syncToken{}, false
	}
	logID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return syncToken{}, false
	}
	day, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return syncToken{}, false
	}
	return syncToken{calendar: parts[0], logID: logID, day: day, version: parts[3]}, true
}

// today is the UTC day number a token is issued on.
func today() int64 { return time.Now().UTC().Unix() / 86400 }

func (b *backend) maxLogID(ctx context.Context, cal generated.Calendar) (uint64, error) {
	id, err := b.s.deps.Queries.GetMaxCalendarLogID(ctx, generated.GetMaxCalendarLogIDParams{
		WorkspaceID: b.s.deps.WorkspaceID,
		CalendarID:  sql.NullInt32{Int32: int32(cal.ID), Valid: true},
	})
	return uint64(id), err
}

func (b *backend) currentToken(ctx context.Context, cal generated.Calendar, day int64) (string, error) {
	id, err := b.maxLogID(ctx, cal)
	if err != nil {
		return "", err
	}
	return syncToken{calendar: publicUUID(cal.PublicID).String(), logID: id, day: day, version: ical.RendererVersion}.String(), nil
}

// SyncCollection implements caldav.SyncBackend.
func (b *backend) SyncCollection(ctx context.Context, p string, query *caldav.SyncQuery) (*caldav.SyncResponse, error) {
	cal, _, _, err := b.calendarAt(ctx, p, levelCalendar)
	if err != nil {
		return nil, err
	}
	day := today()
	maxID, err := b.maxLogID(ctx, cal)
	if err != nil {
		return nil, err
	}
	calUUID := publicUUID(cal.PublicID).String()
	resp := &caldav.SyncResponse{
		SyncToken: syncToken{calendar: calUUID, logID: maxID, day: day, version: ical.RendererVersion}.String(),
	}

	if query.SyncToken == "" {
		objs, err := b.allObjects(ctx, cal, icsexport.LoadOptions{})
		if err != nil {
			return nil, err
		}
		if query.Limit > 0 && len(objs) > query.Limit {
			return nil, caldav.ErrSyncLimitExceeded
		}
		resp.Updated = objs
		return resp, nil
	}

	tok, ok := parseToken(query.SyncToken)
	if !ok || tok.calendar != calUUID || tok.day != day || tok.version != ical.RendererVersion || tok.logID > maxID {
		return nil, caldav.ErrInvalidSyncToken
	}
	subjects, err := b.changedSubjects(ctx, cal, tok.logID)
	if err != nil {
		return nil, err
	}
	for _, subject := range subjects {
		updated, deleted, err := b.syncSubject(ctx, cal, subject)
		if err != nil {
			return nil, err
		}
		if updated != nil {
			resp.Updated = append(resp.Updated, *updated)
		}
		if deleted != "" {
			resp.Deleted = append(resp.Deleted, deleted)
		}
	}
	if query.Limit > 0 && len(resp.Updated)+len(resp.Deleted) > query.Limit {
		return nil, caldav.ErrSyncLimitExceeded
	}
	return resp, nil
}

// changedSubjects reads the log past logID and returns the series it says may
// have changed, in log order. Rows past logID are judged in full; rows at or
// below it inside the reread window only name series to redraw, so a row
// that already forced a full resync cannot force another.
func (b *backend) changedSubjects(ctx context.Context, cal generated.Calendar, logID uint64) ([][]byte, error) {
	q := b.s.deps.Queries
	since := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if logID > 0 {
		row, err := q.GetCalendarLogRow(ctx, logID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, caldav.ErrInvalidSyncToken
		}
		if err != nil {
			return nil, err
		}
		if row.WorkspaceID != b.s.deps.WorkspaceID ||
			!(row.CalendarID.Valid && uint32(row.CalendarID.Int32) == cal.ID ||
				!row.CalendarID.Valid && row.Type == eventlog.TypeWorkspaceMemberRemoved) {
			return nil, caldav.ErrInvalidSyncToken
		}
		since = row.OccurredAt.Add(-rereadWindow)
	}

	var subjects [][]byte
	seen := map[string]bool{}
	newRows := 0
	minID := logID
	for {
		rows, err := q.ListCalendarLogSince(ctx, generated.ListCalendarLogSinceParams{
			WorkspaceID: b.s.deps.WorkspaceID,
			CalendarID:  sql.NullInt32{Int32: int32(cal.ID), Valid: true},
			MinID:       minID,
			Since:       since,
			Limit:       syncPageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			action := logActions[row.Type]
			if row.ID > logID {
				newRows++
				if newRows > maxSyncRows {
					return nil, caldav.ErrInvalidSyncToken
				}
				switch action {
				case actionResync:
					return nil, caldav.ErrInvalidSyncToken
				case actionResyncIfReader:
					if b.namesReader(row.PayloadJSON) {
						return nil, caldav.ErrInvalidSyncToken
					}
				}
			}
			if action == actionRender && len(row.SubjectPublicID) > 0 && !seen[string(row.SubjectPublicID)] {
				seen[string(row.SubjectPublicID)] = true
				subjects = append(subjects, row.SubjectPublicID)
			}
		}
		if len(rows) < syncPageSize {
			return subjects, nil
		}
		// Later pages hold only rows past the last one read; the reread rows
		// all sort before them and came with the first page.
		minID = rows[len(rows)-1].ID
		since = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	}
}

// namesReader reports whether a role change payload is about the reader.
func (b *backend) namesReader(payload json.RawMessage) bool {
	var p struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		// A payload that cannot be read might be about anyone.
		return true
	}
	return strings.EqualFold(p.UserID, b.userUUID)
}

// syncSubject decides how one changed series is reported: as updated when the
// reader now sees it on cal, as deleted when it is gone from cal but the
// reader saw it there, and not at all otherwise. A series purged outright
// cannot be named and is left to the daily full resync, as is one still on cal
// that has left the reader's view.
func (b *backend) syncSubject(ctx context.Context, cal generated.Calendar, subject []byte) (*caldav.CalendarObject, string, error) {
	q := b.s.deps.Queries
	head, err := q.GetCalendarEventByPublicIDAny(ctx, generated.GetCalendarEventByPublicIDAnyParams{
		WorkspaceID: b.s.deps.WorkspaceID, PublicID: subject,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if head.RecurrenceParentID.Valid {
		// A changed occurrence is reported through its series. Once the
		// series itself is gone, its own log row reports that.
		head, err = q.GetCalendarEventByID(ctx, uint32(head.RecurrenceParentID.Int32))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
	}

	if head.Enabled && head.CalendarID == cal.ID {
		s, ok, err := icsexport.LoadSeries(ctx, q, b.viewer(), cal, head.PublicID, icsexport.LoadOptions{})
		if err != nil || !ok {
			return nil, "", err
		}
		res, err := b.resourceOf(ctx, cal, head)
		if err != nil {
			return nil, "", err
		}
		co, err := b.object(cal, *s, res)
		if err != nil {
			return nil, "", err
		}
		return &co, "", nil
	}

	// Deleted, or moved to another calendar: judged by cal's rules as it was.
	_, saw, err := icsexport.LoadSeries(ctx, q, b.viewer(), cal, head.PublicID, icsexport.LoadOptions{IncludeDisabled: true})
	if err != nil || !saw {
		return nil, "", err
	}
	res, err := b.resourceOf(ctx, cal, head)
	if err != nil {
		return nil, "", err
	}
	return nil, b.calendarPath(publicUUID(cal.PublicID)) + res.name, nil
}
