package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// mxRole is who makes a request in the authorization matrix.
type mxRole string

const (
	mxOutsider mxRole = "non-member"
	mxViewer   mxRole = "viewer"
	mxEditor   mxRole = "editor"
	mxManager  mxRole = "manager"
	mxOwner    mxRole = "owner"
)

// mxLadder is every authenticated calendar role, least privileged first.
var mxLadder = []mxRole{mxOutsider, mxViewer, mxEditor, mxManager, mxOwner}

var (
	mxRead   = []mxRole{mxViewer, mxEditor, mxManager, mxOwner}
	mxWrite  = []mxRole{mxEditor, mxManager, mxOwner}
	mxManage = []mxRole{mxManager, mxOwner}
	mxOwn    = []mxRole{mxOwner}
)

// mxGroup is the router group an operation is registered in.
type mxGroup int

const (
	mxPublic mxGroup = iota
	mxProtected
	mxAdmin
)

// mxChild names the resource a row's deepest path parameter points at, which
// is what a foreign id is substituted for.
type mxChild string

const (
	mxNoChild    mxChild = ""
	mxEvent      mxChild = "event"
	mxMemo       mxChild = "memo"
	mxItem       mxChild = "checklist item"
	mxComment    mxChild = "comment"
	mxAttachment mxChild = "attachment"
	mxReserved   mxChild = "unconfirmed attachment"
	mxPhoto      mxChild = "photo"
	mxPending    mxChild = "unconfirmed photo"
	mxInvite     mxChild = "invite"
	mxMember     mxChild = "member"
	// mxTrashed is a deleted event, which the restore path names.
	mxTrashed mxChild = "deleted event"
	// mxSeries is a recurring event with one cancelled occurrence.
	mxSeries mxChild = "series"
	// mxCover is a cover reserved and uploaded but not yet confirmed.
	mxCover mxChild = "unconfirmed cover"
)

// mxCancelledDate is the occurrence of the mxSeries fixture that is cancelled.
const mxCancelledDate = "2026-06-17"

// mxRow is one operation's authorization contract.
//
// A calendar-scoped row (allow non-nil) is driven against a fresh calendar
// with a member at every role: each role outside allow is refused with 403 and
// leaves the calendar as it was, a foreign child id is answered 404, and the
// least privileged allowed role gets through. Other rows are checked by group
// alone.
type mxRow struct {
	method string
	path   string
	group  mxGroup
	allow  []mxRole
	child  mxChild
	// storage marks rows whose fixture or allowed request needs object storage.
	storage bool
	// query is appended to the request path.
	query string
	// body builds the request body; nil sends none.
	body func(w *mxWorld) any
	// userID names the member a /members/{userId} or /attendees/{userId} path acts on.
	userID func(w *mxWorld) string
	// optional rows are registered only in some configurations.
	optional bool
	// logScoped rows read the activity log, which is filtered by calendar
	// rather than by looking the subject up -- a deleted event's history stays
	// readable -- so a foreign id reads as an empty history instead of a 404.
	logScoped bool
	// Schedule polls authorize recipients and the assigned organizer individually.
	pollScoped bool
}

func (r mxRow) key() string { return r.method + " " + r.path }

func mxJSON(v map[string]any) func(*mxWorld) any {
	return func(*mxWorld) any { return v }
}

// mxEventStart and mxEventEnd fall inside the June listing a snapshot reads, as
// does the event in mxICS, so an import a refused request let through shows.
const (
	mxEventStart = "2026-06-10T10:00:00+09:00"
	mxEventEnd   = "2026-06-10T11:00:00+09:00"
	mxICS        = "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:matrix@test\r\nDTSTART:20260611T090000Z\r\n" +
		"DTEND:20260611T100000Z\r\nSUMMARY:Imported\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
)

func mxRows() []mxRow {
	eventUpdate := func(w *mxWorld) any {
		return eventBody("Matrix event moved", "2026-06-10T12:00:00+09:00", "2026-06-10T13:00:00+09:00", w.attendees()...)
	}
	subject := func(w *mxWorld) string { return w.subject.UserID }
	attendee := func(w *mxWorld) string { return w.users[mxViewer].UserID }
	attachmentPresign := func(*mxWorld) any {
		b := uniqueAttachmentBody("matrix presign")
		return map[string]any{
			"filename": "matrix.pdf", "contentType": "application/pdf",
			"byteSize": len(b), "sha256": helpers.SHA256Hex(b),
		}
	}
	photoPresign := func(*mxWorld) any {
		png := append(helpers.TinyPNG(), fmt.Appendf(nil, "matrix-%d-%d", photoRunID, uniquePhotoSuffix.Add(1))...)
		return map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)}
	}
	availabilityRequest := func(w *mxWorld) map[string]any {
		start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)
		return map[string]any{
			"timezone": "UTC", "memberIds": []string{w.users[mxViewer].UserID},
			"slots": []map[string]string{scheduleSlot(start)},
		}
	}
	pollRequest := func(w *mxWorld) any {
		body := availabilityRequest(w)
		body["title"] = "Matrix poll"
		return body
	}

	const (
		cal   = "/calendars/{calendarId}"
		evt   = cal + "/events/{eventId}"
		polls = cal + "/schedule-polls"
		poll  = polls + "/{pollId}"
	)
	return []mxRow{
		// Public: no credentials asked for.
		{method: "POST", path: "/auth/register", group: mxPublic},
		{method: "POST", path: "/auth/login", group: mxPublic},
		{method: "POST", path: "/auth/refresh", group: mxPublic},
		{method: "POST", path: "/auth/dev-login", group: mxPublic, optional: true},
		{method: "POST", path: "/auth/password-reset/request", group: mxPublic},
		{method: "POST", path: "/auth/password-reset/confirm", group: mxPublic},
		{method: "POST", path: "/auth/verify-email/confirm", group: mxPublic},
		{method: "GET", path: "/auth/oauth/providers", group: mxPublic},
		{method: "GET", path: "/auth/oauth/{provider}/start", group: mxPublic},
		{method: "GET", path: "/auth/oauth/{provider}/callback", group: mxPublic},
		{method: "GET", path: "/share/{token}", group: mxPublic},
		{method: "GET", path: "/share/{token}/events", group: mxPublic},
		{method: "GET", path: "/auth/app-password-profiles/{ticket}", group: mxPublic},

		// Protected, scoped to the caller rather than a calendar.
		{method: "GET", path: "/user", group: mxProtected},
		{method: "PATCH", path: "/user", group: mxProtected},
		{method: "POST", path: "/user/verify-email/resend", group: mxProtected},
		{method: "POST", path: "/auth/logout", group: mxProtected},
		{method: "GET", path: "/user/sessions", group: mxProtected},
		{method: "DELETE", path: "/user/sessions/{sessionId}", group: mxProtected},
		{method: "PUT", path: "/user/password", group: mxProtected},
		{method: "POST", path: "/user/avatar/presign", group: mxProtected},
		{method: "PUT", path: "/user/avatar", group: mxProtected},
		{method: "DELETE", path: "/user/avatar", group: mxProtected},
		{method: "GET", path: "/user/app-passwords", group: mxProtected},
		{method: "POST", path: "/user/app-passwords", group: mxProtected},
		{method: "POST", path: "/user/app-passwords/profile-links", group: mxProtected},
		{method: "DELETE", path: "/user/app-passwords/{appPasswordId}", group: mxProtected},
		{method: "GET", path: "/calendars", group: mxProtected},
		{method: "POST", path: "/calendars", group: mxProtected},
		{method: "POST", path: "/invites/{token}/accept", group: mxProtected},
		{method: "GET", path: "/user/schedule-poll-actions", group: mxProtected},
		{method: "GET", path: "/search", group: mxProtected},

		// Calendar-scoped.
		{method: "GET", path: cal, group: mxProtected, allow: mxRead},
		{method: "PATCH", path: cal, group: mxProtected, allow: mxManage, body: mxJSON(map[string]any{"name": "Renamed"})},
		{method: "DELETE", path: cal, group: mxProtected, allow: mxOwn},
		{method: "GET", path: cal + "/members", group: mxProtected, allow: mxRead},
		{method: "POST", path: cal + "/members", group: mxProtected, allow: mxManage, body: func(w *mxWorld) any {
			return map[string]any{"userId": w.a.stranger.UserID, "role": "viewer"}
		}},
		{method: "GET", path: cal + "/member-candidates", group: mxProtected, allow: mxManage},
		{method: "PUT", path: cal + "/members/{userId}/role", group: mxProtected, allow: mxManage, child: mxMember,
			userID: subject, body: mxJSON(map[string]any{"role": "viewer"})},
		{method: "PUT", path: cal + "/members/{userId}/color", group: mxProtected, allow: mxManage, child: mxMember,
			userID: subject, body: mxJSON(map[string]any{"color": "#F35F8C"})},
		{method: "DELETE", path: cal + "/members/{userId}", group: mxProtected, allow: mxManage, child: mxMember, userID: subject},
		{method: "GET", path: cal + "/labels", group: mxProtected, allow: mxRead},
		{method: "GET", path: cal + "/export", group: mxProtected, allow: mxRead},
		{method: "POST", path: cal + "/import", group: mxProtected, allow: mxWrite, body: mxJSON(map[string]any{"ics": mxICS})},
		{method: "GET", path: cal + "/activity", group: mxProtected, allow: mxRead},
		{method: "POST", path: cal + "/cover/presign", group: mxProtected, allow: mxManage, storage: true,
			body: func(w *mxWorld) any {
				image := newCalendarCoverPNG(w.t)
				return map[string]any{"sha256": helpers.SHA256Hex(image), "contentType": "image/png", "byteSize": len(image)}
			}},
		{method: "PUT", path: cal + "/cover", group: mxProtected, allow: mxManage, storage: true,
			body: func(w *mxWorld) any { return map[string]any{"coverId": w.child(mxCover)} }},
		{method: "DELETE", path: cal + "/cover", group: mxProtected, allow: mxManage, storage: true},

		{method: "GET", path: polls, group: mxProtected, allow: mxRead},
		{method: "GET", path: polls + "/availability-sharing", group: mxProtected, allow: mxManage},
		{method: "PUT", path: polls + "/availability-sharing", group: mxProtected, allow: mxManage, body: mxJSON(map[string]any{"enabled": false})},
		{method: "POST", path: polls + "/availability", group: mxProtected, allow: mxManage, body: func(w *mxWorld) any { return availabilityRequest(w) }},
		{method: "POST", path: polls, group: mxProtected, allow: mxManage, body: pollRequest},
		{method: "GET", path: poll, group: mxProtected, pollScoped: true},
		{method: "PATCH", path: poll, group: mxProtected, pollScoped: true},
		{method: "POST", path: poll + "/refresh-availability", group: mxProtected, pollScoped: true},
		{method: "GET", path: poll + "/response", group: mxProtected, pollScoped: true},
		{method: "PUT", path: poll + "/response", group: mxProtected, pollScoped: true},
		{method: "POST", path: poll + "/finalize", group: mxProtected, pollScoped: true},
		{method: "POST", path: poll + "/cancel", group: mxProtected, pollScoped: true},

		{method: "GET", path: cal + "/events", group: mxProtected, allow: mxRead, query: "?start=2026-06-01&end=2026-06-30"},
		{method: "POST", path: cal + "/events", group: mxProtected, allow: mxWrite,
			body: mxJSON(map[string]any{"title": "Created", "allDay": false, "startAt": mxEventStart, "endAt": mxEventEnd})},
		{method: "GET", path: evt, group: mxProtected, allow: mxRead, child: mxEvent},
		{method: "PATCH", path: evt, group: mxProtected, allow: mxManage, child: mxEvent, body: eventUpdate},
		{method: "DELETE", path: evt, group: mxProtected, allow: mxManage, child: mxEvent},
		{method: "POST", path: evt + "/restore", group: mxProtected, allow: mxManage, child: mxTrashed},
		{method: "DELETE", path: evt + "/exceptions/{occurrenceDate}", group: mxProtected, allow: mxManage, child: mxSeries},
		{method: "PUT", path: evt + "/rsvp", group: mxProtected, allow: mxRead, child: mxEvent,
			body: mxJSON(map[string]any{"rsvp": "accepted"})},
		{method: "PUT", path: evt + "/attendees/{userId}", group: mxProtected, allow: mxManage, child: mxEvent,
			userID: attendee, body: mxJSON(map[string]any{"canEdit": true})},
		{method: "GET", path: evt + "/history", group: mxProtected, allow: mxRead, child: mxEvent, logScoped: true},

		{method: "GET", path: evt + "/activities", group: mxProtected, allow: mxRead, child: mxEvent},
		{method: "POST", path: evt + "/activities", group: mxProtected, allow: mxWrite, child: mxEvent,
			body: mxJSON(map[string]any{"body": "Added"})},
		// Only the author edits a comment; the fixture's author is the owner.
		{method: "PATCH", path: evt + "/activities/{commentId}", group: mxProtected, allow: mxOwn, child: mxComment,
			body: mxJSON(map[string]any{"body": "Edited"})},
		{method: "DELETE", path: evt + "/activities/{commentId}", group: mxProtected, allow: mxManage, child: mxComment},

		{method: "GET", path: evt + "/checklist", group: mxProtected, allow: mxRead, child: mxEvent},
		{method: "POST", path: evt + "/checklist", group: mxProtected, allow: mxManage, child: mxEvent,
			body: mxJSON(map[string]any{"title": "Added"})},
		{method: "PATCH", path: evt + "/checklist/{itemId}", group: mxProtected, allow: mxManage, child: mxItem,
			body: mxJSON(map[string]any{"done": true})},
		{method: "DELETE", path: evt + "/checklist/{itemId}", group: mxProtected, allow: mxManage, child: mxItem},

		{method: "GET", path: evt + "/attachments", group: mxProtected, allow: mxRead, child: mxEvent},
		{method: "POST", path: evt + "/attachments/presign", group: mxProtected, allow: mxManage, child: mxEvent,
			storage: true, body: attachmentPresign},
		{method: "POST", path: evt + "/attachments/{attachmentId}/confirm", group: mxProtected, allow: mxManage,
			child: mxReserved, storage: true},
		{method: "GET", path: evt + "/attachments/{attachmentId}/download", group: mxProtected, allow: mxRead,
			child: mxAttachment, storage: true},
		{method: "DELETE", path: evt + "/attachments/{attachmentId}", group: mxProtected, allow: mxManage,
			child: mxAttachment, storage: true},

		{method: "GET", path: cal + "/albums", group: mxProtected, allow: mxRead},
		{method: "POST", path: cal + "/albums/presign", group: mxProtected, allow: mxWrite, storage: true, body: photoPresign},
		{method: "POST", path: cal + "/albums/{photoId}/confirm", group: mxProtected, allow: mxWrite, child: mxPending, storage: true},
		{method: "PATCH", path: cal + "/albums/{photoId}", group: mxProtected, allow: mxWrite, child: mxPhoto, storage: true,
			body: mxJSON(map[string]any{"caption": "Captioned"})},
		{method: "DELETE", path: cal + "/albums/{photoId}", group: mxProtected, allow: mxWrite, child: mxPhoto, storage: true},
		{method: "GET", path: cal + "/albums/{photoId}/download", group: mxProtected, allow: mxRead, child: mxPhoto, storage: true},

		{method: "GET", path: cal + "/memos", group: mxProtected, allow: mxRead},
		{method: "POST", path: cal + "/memos", group: mxProtected, allow: mxWrite, body: mxJSON(map[string]any{"title": "Added"})},
		{method: "PATCH", path: cal + "/memos/{memoId}", group: mxProtected, allow: mxWrite, child: mxMemo,
			body: mxJSON(map[string]any{"title": "Edited"})},
		{method: "DELETE", path: cal + "/memos/{memoId}", group: mxProtected, allow: mxWrite, child: mxMemo},
		{method: "GET", path: cal + "/memos/{memoId}/history", group: mxProtected, allow: mxRead, child: mxMemo, logScoped: true},

		{method: "POST", path: cal + "/invites", group: mxProtected, allow: mxManage, body: mxJSON(map[string]any{"role": "viewer"})},
		{method: "GET", path: cal + "/invites", group: mxProtected, allow: mxManage},
		{method: "DELETE", path: cal + "/invites/{inviteId}", group: mxProtected, allow: mxManage, child: mxInvite},

		// Admin.
		{method: "GET", path: "/admin/oauth-providers", group: mxAdmin},
		{method: "PATCH", path: "/admin/oauth-providers/{provider}", group: mxAdmin},
		{method: "DELETE", path: "/admin/oauth-providers/{provider}", group: mxAdmin},
		{method: "GET", path: "/admin/instance-admins", group: mxAdmin},
		{method: "POST", path: "/admin/instance-admins", group: mxAdmin},
		{method: "DELETE", path: "/admin/instance-admins/{userId}", group: mxAdmin},
		{method: "GET", path: "/admin/allowed-emails", group: mxAdmin},
		{method: "POST", path: "/admin/allowed-emails", group: mxAdmin},
		{method: "DELETE", path: "/admin/allowed-emails/{emailId}", group: mxAdmin},
	}
}

// mxActors are the accounts every row shares. Each calendar-scoped row gets a
// calendar of its own, so what one row's allowed request does cannot reach
// another's.
type mxActors struct {
	users    map[mxRole]*helpers.TestTenant
	subject  *helpers.TestTenant // the member that member-management routes act on
	stranger *helpers.TestTenant // owns the calendar foreign ids are sent under
	// strangerEvent is an event on the stranger's calendar, for paths that need
	// one before the substituted child.
	strangerEvent string
}

// mxWorld is one row's calendar and the fixtures on it, all made by its owner.
type mxWorld struct {
	t      *testing.T
	a      *mxActors
	users  map[mxRole]*helpers.TestTenant
	calID  string
	calURL string
	event  string
	// sibling is a second event on the same calendar, for sending one event's
	// child under another event's path.
	sibling  string
	subject  *helpers.TestTenant
	children map[mxChild]string
}

func (w *mxWorld) attendees() []string {
	return []string{w.users[mxOwner].UserID, w.users[mxManager].UserID, w.users[mxEditor].UserID, w.users[mxViewer].UserID}
}

func (w *mxWorld) owner() string { return w.users[mxOwner].AccessToken }

func newMxWorld(t *testing.T, a *mxActors) *mxWorld {
	t.Helper()
	owner := a.users[mxOwner]
	var cal struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", owner.AccessToken,
		map[string]any{"name": "Matrix", "color": helpers.TenantCalendarColor}, &cal)
	w := &mxWorld{
		t: t, a: a, users: a.users, subject: a.subject,
		calID: cal.ID, calURL: testServerURL + "/calendars/" + cal.ID,
		children: map[mxChild]string{},
	}
	// An invite grants editor at most, so the manager joins as one and is promoted.
	joinAs(t, w.calURL, owner.AccessToken, a.users[mxViewer], "viewer")
	joinAs(t, w.calURL, owner.AccessToken, a.users[mxEditor], "editor")
	joinAs(t, w.calURL, owner.AccessToken, a.users[mxManager], "editor")
	joinAs(t, w.calURL, owner.AccessToken, a.subject, "editor")
	require.Equal(t, 200, promote(t, w.calURL, owner.AccessToken, a.users[mxManager], "manager"))

	w.event = w.createEvent("Matrix event", w.attendees())
	w.sibling = w.createEvent("Matrix sibling", nil)
	return w
}

func (w *mxWorld) createEvent(title string, participants []string) string {
	w.t.Helper()
	body := map[string]any{"title": title, "allDay": false, "startAt": mxEventStart, "endAt": mxEventEnd}
	if participants != nil {
		body["participantIds"] = participants
	}
	var e struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/events", w.owner(), body, &e)
	return e.ID
}

// child returns the fixture a row's deepest path parameter names, creating it
// on first use.
func (w *mxWorld) child(kind mxChild) string {
	w.t.Helper()
	if id, ok := w.children[kind]; ok {
		return id
	}
	var id string
	switch kind {
	case mxEvent:
		id = w.event
	case mxMember:
		id = w.subject.UserID
	case mxMemo:
		var m struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/memos", w.owner(), map[string]any{"title": "Matrix memo", "body": "kept"}, &m)
		id = m.ID
	case mxItem:
		var it struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/events/"+w.event+"/checklist", w.owner(), map[string]any{"title": "Matrix item"}, &it)
		id = it.ID
	case mxComment:
		var c struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/events/"+w.event+"/activities", w.owner(), map[string]any{"body": "Matrix comment"}, &c)
		id = c.ID
	case mxTrashed:
		id = w.createEvent("Matrix deleted", nil)
		status, raw := w.do(http.MethodDelete, w.calURL+"/events/"+id, w.owner(), nil)
		require.Equal(w.t, http.StatusNoContent, status, raw)
	case mxSeries:
		var e struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/events", w.owner(), map[string]any{
			"title": "Matrix series", "allDay": false, "startAt": mxEventStart, "endAt": mxEventEnd,
			"recurrenceRule": map[string]any{"freq": "weekly", "interval": 1, "count": 4},
		}, &e)
		id = e.ID
		status, raw := w.do(http.MethodDelete, w.calURL+"/events/"+id+"_"+strings.ReplaceAll(mxCancelledDate, "-", "")+"?scope=this", w.owner(), nil)
		require.Equal(w.t, http.StatusNoContent, status, raw)
	case mxCover:
		// Only a manager may reserve a cover, and the row confirms it as one.
		image := newCalendarCoverPNG(w.t)
		var pres struct {
			CoverID   string `json:"coverId"`
			UploadURL string `json:"uploadUrl"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/cover/presign", w.users[mxManager].AccessToken, map[string]any{
			"sha256": helpers.SHA256Hex(image), "contentType": "image/png", "byteSize": len(image),
		}, &pres)
		require.NotEmpty(w.t, pres.UploadURL)
		helpers.UploadToSignedURL(w.t, pres.UploadURL, "image/png", image)
		id = pres.CoverID
	case mxInvite:
		var inv struct {
			ID string `json:"id"`
		}
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/invites", w.owner(), map[string]any{"role": "viewer"}, &inv)
		id = inv.ID
	case mxAttachment, mxReserved:
		// Only the uploader may confirm an upload, so the one left unconfirmed
		// is the least privileged allowed role's, which its row then confirms.
		uploader := w.owner()
		if kind == mxReserved {
			uploader = w.users[mxManager].AccessToken
		}
		body := uniqueAttachmentBody("matrix " + string(kind))
		var pres presignResp
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/events/"+w.event+"/attachments/presign", uploader,
			map[string]any{
				"filename": "matrix.pdf", "contentType": "application/pdf",
				"byteSize": len(body), "sha256": helpers.SHA256Hex(body),
			}, &pres)
		require.NotEmpty(w.t, pres.UploadURL)
		helpers.UploadToSignedURL(w.t, pres.UploadURL, "application/pdf", body)
		if kind == mxAttachment {
			helpers.DoJSON(w.t, http.MethodPost,
				w.calURL+"/events/"+w.event+"/attachments/"+pres.AttachmentID+"/confirm", w.owner(), nil, nil)
		}
		id = pres.AttachmentID
	case mxPhoto, mxPending:
		uploader := w.owner()
		if kind == mxPending {
			uploader = w.users[mxEditor].AccessToken
		}
		png := append(helpers.TinyPNG(), fmt.Appendf(nil, "matrix-%d-%d", photoRunID, uniquePhotoSuffix.Add(1))...)
		var pres albumPresignResp
		helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/albums/presign", uploader,
			map[string]any{"contentType": "image/png", "byteSize": len(png), "sha256": helpers.SHA256Hex(png)}, &pres)
		helpers.UploadToSignedURL(w.t, pres.UploadURL, "image/png", png)
		if kind == mxPhoto {
			helpers.DoJSON(w.t, http.MethodPost, w.calURL+"/albums/"+pres.PhotoID+"/confirm", w.owner(), nil, nil)
		}
		id = pres.PhotoID
	default:
		w.t.Fatalf("no fixture for %q", kind)
	}
	w.children[kind] = id
	return id
}

var mxParam = regexp.MustCompile(`\{([A-Za-z]+)\}`)

// url fills a row's path for this world. calendarID and eventID replace the
// calendar and event when non-empty, which is how a foreign id is substituted.
func (w *mxWorld) url(r mxRow, calendarID, eventID string, childID string) string {
	w.t.Helper()
	if calendarID == "" {
		calendarID = w.calID
	}
	if eventID == "" {
		eventID = w.event
	}
	path := mxParam.ReplaceAllStringFunc(r.path, func(m string) string {
		switch name := m[1 : len(m)-1]; name {
		case "calendarId":
			return calendarID
		case "eventId":
			switch r.child {
			case mxTrashed, mxSeries:
				if childID != "" {
					return childID
				}
				return w.child(r.child)
			case mxEvent:
				if childID != "" {
					return childID
				}
			}
			return eventID
		case "occurrenceDate":
			return mxCancelledDate
		case "userId":
			if r.child == mxMember && childID != "" {
				return childID
			}
			return r.userID(w)
		case "memoId", "itemId", "commentId", "attachmentId", "photoId", "inviteId":
			if childID != "" {
				return childID
			}
			return w.child(r.child)
		default:
			w.t.Fatalf("unfilled parameter %s in %s", name, r.path)
			return ""
		}
	})
	return testServerURL + path + r.query
}

// snapshot is everything the owner can read about the calendar, with signed
// URLs blanked: two reads of an unchanged calendar compare equal, and any
// write a refused request let through does not.
func (w *mxWorld) snapshot() string {
	w.t.Helper()
	reads := []string{
		w.calURL,
		w.calURL + "/members",
		w.calURL + "/events?start=2026-06-01&end=2026-06-30",
		w.calURL + "/memos",
		w.calURL + "/invites",
		w.calURL + "/events/" + w.event + "/checklist",
		w.calURL + "/events/" + w.event + "/activities",
		w.calURL + "/events/" + w.event + "/attachments",
	}
	if helpers.StorageEnabled() {
		reads = append(reads, w.calURL+"/albums")
	}
	var sb strings.Builder
	for _, u := range reads {
		status, raw := helpers.DoJSONStatus(w.t, http.MethodGet, u, w.owner(), nil)
		var v any
		if status == 200 {
			require.NoError(w.t, json.Unmarshal(raw, &v))
		}
		out, err := json.Marshal(mxBlankURLs(v))
		require.NoError(w.t, err)
		fmt.Fprintf(&sb, "%s %d %s\n", strings.TrimPrefix(u, w.calURL), status, out)
	}
	return sb.String()
}

func mxBlankURLs(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = mxBlankURLs(e)
		}
	case []any:
		for i, e := range x {
			x[i] = mxBlankURLs(e)
		}
	case string:
		if strings.HasPrefix(x, "http://") || strings.HasPrefix(x, "https://") {
			return "<url>"
		}
	}
	return v
}

// do sends a request. Every write names "any version", so a refusal under
// test is the authorization one rather than a missing precondition.
func (w *mxWorld) do(method, url, token string, body any) (int, string) {
	w.t.Helper()
	var headers map[string]string
	if method != http.MethodGet {
		headers = ifMatch("*")
	}
	status, raw := helpers.DoJSONStatusWithHeaders(w.t, method, url, token, body, headers)
	return status, string(raw)
}

func (r mxRow) bodyFor(w *mxWorld) any {
	if r.body == nil {
		return nil
	}
	return r.body(w)
}

// runCalendarRow drives one calendar-scoped row through every role and every
// foreign id it can be handed.
func runCalendarRow(t *testing.T, a *mxActors, r mxRow) {
	w := newMxWorld(t, a)
	target := w.url(r, "", "", "")
	before := w.snapshot()

	allowed := map[mxRole]bool{}
	for _, role := range r.allow {
		allowed[role] = true
	}
	for _, role := range mxLadder {
		if allowed[role] {
			continue
		}
		token := a.stranger.AccessToken
		if role != mxOutsider {
			token = a.users[role].AccessToken
		}
		status, body := w.do(r.method, target, token, r.bodyFor(w))
		require.Equal(t, http.StatusForbidden, status, "%s must refuse a %s: %s", r.key(), role, body)
	}
	require.Equal(t, before, w.snapshot(), "%s: a refused request changed the calendar", r.key())

	if r.child != mxNoChild {
		// A child of this calendar sent under the stranger's own calendar,
		// where the stranger may do anything.
		foreign := w.url(r, a.stranger.CalendarID, a.strangerEvent, w.childID(r))
		status, body := w.do(r.method, foreign, a.stranger.AccessToken, r.bodyFor(w))
		if r.logScoped {
			require.Equal(t, http.StatusOK, status, body)
			empty := `{"items":[]}`
			if r.child == mxMemo {
				empty = "[]"
			}
			require.JSONEq(t, empty, body, "%s must not read the log of a %s of another calendar", r.key(), r.child)
		} else {
			require.Equal(t, http.StatusNotFound, status, "%s must not reach a %s of another calendar: %s", r.key(), r.child, body)
		}

		// A child of one event sent under a sibling event on the same calendar.
		if strings.Contains(r.path, "{eventId}/") && r.child != mxEvent && r.child != mxTrashed && r.child != mxSeries {
			sibling := w.url(r, "", w.sibling, "")
			status, body := w.do(r.method, sibling, w.owner(), r.bodyFor(w))
			require.Equal(t, http.StatusNotFound, status, "%s must not reach a %s of another event: %s", r.key(), r.child, body)
		}
		require.Equal(t, before, w.snapshot(), "%s: a foreign id changed the calendar", r.key())
	}

	least := r.allow[0]
	status, body := w.do(r.method, target, a.users[least].AccessToken, r.bodyFor(w))
	require.True(t, status >= 200 && status < 300, "%s must admit a %s: %d %s", r.key(), least, status, body)
}

func (w *mxWorld) childID(r mxRow) string {
	if r.child == mxMember {
		return w.subject.UserID
	}
	return w.child(r.child)
}

// TestAuthzMatrix is the authorization contract of the whole API, one row per
// operation the OpenAPI document describes.
//
// Every operation must have a row, so a route added without deciding who may
// call it fails here. Outside the public group a request without credentials
// is refused by the auth middleware, and an admin route refuses an ordinary
// account. A calendar-scoped row is driven per role and per substituted
// foreign id; see mxRow.
func TestAuthzMatrix(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	rows := mxRows()
	byKey := map[string]mxRow{}
	for _, r := range rows {
		require.NotContains(t, byKey, r.key(), "duplicate row")
		byKey[r.key()] = r
	}

	status, body := getRaw(t, testServerURL+"/openapi.json")
	require.Equal(t, 200, status)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &doc))
	var ops []string
	for path, methods := range doc.Paths {
		for m := range methods {
			if m == "parameters" {
				continue
			}
			ops = append(ops, strings.ToUpper(m)+" "+path)
		}
	}
	sort.Strings(ops)
	documented := map[string]bool{}
	for _, op := range ops {
		documented[op] = true
		require.Contains(t, byKey, op, "operation %s has no row in the authorization matrix", op)
	}
	for _, r := range rows {
		if !r.optional {
			require.True(t, documented[r.key()], "row %s names no documented operation", r.key())
		}
	}

	stranger := helpers.NewTenant(t, testServerURL)
	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	t.Run("group", func(t *testing.T) {
		for _, op := range ops {
			r := byKey[op]
			url := testServerURL + mxParam.ReplaceAllStringFunc(r.path, func(m string) string {
				if m == "{provider}" {
					return "nonexistent"
				}
				return "019770a0-0000-7000-8000-000000000000"
			})
			status, raw := helpers.DoJSONStatus(t, r.method, url, "", map[string]any{})
			code := mxCode(raw)
			if r.group == mxPublic {
				require.NotEqual(t, "AUTH.TOKEN_MISSING", code, "%s is public and must not ask for a token", op)
				continue
			}
			require.Equal(t, http.StatusUnauthorized, status, "%s must refuse an anonymous caller", op)
			require.Equal(t, "AUTH.TOKEN_MISSING", code, op)

			if r.group != mxAdmin {
				continue
			}
			status, raw = helpers.DoJSONStatus(t, r.method, url, stranger.AccessToken, map[string]any{})
			require.Equal(t, http.StatusForbidden, status, "%s must refuse a non-admin", op)
			require.Equal(t, "AUTH.ADMIN_REQUIRED", mxCode(raw), op)
			// An empty body and placeholder ids get past the gate and stop at
			// validation or lookup, so nothing is changed.
			status, raw = helpers.DoJSONStatus(t, r.method, url, admin.AccessToken, map[string]any{})
			require.NotContains(t, []int{401, 403}, status, "%s must admit an admin: %s", op, raw)
		}
	})

	t.Run("caller-scoped", func(t *testing.T) {
		for _, r := range rows {
			if r.group != mxProtected || r.allow != nil || r.pollScoped {
				continue
			}
			// Logging out ends the session, so it gets an account of its own.
			token := stranger.AccessToken
			if r.key() == "POST /auth/logout" {
				token = helpers.NewTenant(t, testServerURL).AccessToken
			}
			url := testServerURL + mxParam.ReplaceAllString(r.path, "019770a0-0000-7000-8000-000000000000")
			status, raw := helpers.DoJSONStatus(t, r.method, url, token, map[string]any{})
			require.NotContains(t, []int{401, 403}, status, "%s must admit any signed-in caller: %s", r.key(), raw)
		}
	})

	owner := helpers.NewTenant(t, testServerURL)
	actors := &mxActors{
		users: map[mxRole]*helpers.TestTenant{
			mxOwner:   owner,
			mxManager: helpers.NewTenant(t, testServerURL),
			mxEditor:  helpers.NewTenant(t, testServerURL),
			mxViewer:  helpers.NewTenant(t, testServerURL),
		},
		subject:  helpers.NewTenant(t, testServerURL),
		stranger: stranger,
	}
	var se struct {
		ID string `json:"id"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars/"+stranger.CalendarID+"/events", stranger.AccessToken,
		map[string]any{"title": "Stranger event", "allDay": false, "startAt": mxEventStart, "endAt": mxEventEnd}, &se)
	actors.strangerEvent = se.ID

	t.Run("calendar", func(t *testing.T) {
		for _, r := range rows {
			if r.allow == nil {
				continue
			}
			t.Run(r.key(), func(t *testing.T) {
				t.Parallel()
				if r.storage {
					requireStorage(t)
				}
				runCalendarRow(t, actors, r)
			})
		}
	})
	t.Run("schedule-poll", func(t *testing.T) {
		for _, r := range rows {
			if !r.pollScoped {
				continue
			}
			t.Run(r.key(), func(t *testing.T) {
				t.Parallel()
				runPollRow(t, actors, r)
			})
		}
	})
}

func runPollRow(t *testing.T, actors *mxActors, row mxRow) {
	w := newMxWorld(t, actors)
	respondent := actors.users[mxViewer]
	base := testServerURL + "/calendars/" + w.calID + "/schedule-polls"
	start := time.Now().UTC().AddDate(0, 0, 7).Truncate(time.Hour)
	var poll schedulePollBody
	helpers.DoJSON(t, http.MethodPost, base, w.owner(), map[string]any{
		"title": "Private matrix poll", "timezone": "UTC", "memberIds": []string{respondent.UserID},
		"slots": []map[string]string{scheduleSlot(start)},
	}, &poll)
	require.Len(t, poll.Slots, 1)
	target := testServerURL + strings.NewReplacer("{calendarId}", w.calID, "{pollId}", poll.ID).Replace(row.path)
	foreign := testServerURL + strings.NewReplacer("{calendarId}", actors.stranger.CalendarID, "{pollId}", poll.ID).Replace(row.path)
	body := map[string]any{}
	answers := []map[string]string{{"slotId": poll.Slots[0].ID, "value": "yes"}}
	organizerOnly := true
	switch {
	case row.method == http.MethodGet, strings.HasSuffix(row.path, "/refresh-availability"):
		body = nil
		organizerOnly = false
	case row.method == http.MethodPatch:
		body["title"] = "Updated matrix poll"
	case strings.HasSuffix(row.path, "/response"):
		body["answers"] = answers
		organizerOnly = false
	case strings.HasSuffix(row.path, "/finalize"):
		doAnyVersion(t, http.MethodPut, base+"/"+poll.ID+"/response", respondent.AccessToken,
			map[string]any{"answers": answers}, nil)
		body["slotId"] = poll.Slots[0].ID
		body["ownerId"] = respondent.UserID
	}

	type refusal struct {
		who    string
		actor  *helpers.TestTenant
		status int
		code   string
	}
	// The stranger is not a member of the poll's calendar.
	refusals := []refusal{{"non-member", actors.stranger, http.StatusForbidden, "CALENDAR.ACCESS_DENIED"}}
	if organizerOnly {
		refusals = append(refusals,
			refusal{"editor", actors.users[mxEditor], http.StatusForbidden, "CALENDAR.ROLE_REQUIRED"},
			refusal{"unassigned manager", actors.users[mxManager], http.StatusForbidden, "SCHEDULE_POLL.ACCESS_DENIED"},
			refusal{"respondent", respondent, http.StatusForbidden, "CALENDAR.ROLE_REQUIRED"},
		)
	} else {
		refusals = append(refusals,
			refusal{"editor", actors.users[mxEditor], http.StatusNotFound, "SCHEDULE_POLL.NOT_FOUND"},
			refusal{"unassigned manager", actors.users[mxManager], http.StatusNotFound, "SCHEDULE_POLL.NOT_FOUND"},
		)
	}
	for _, r := range refusals {
		status, raw := w.do(row.method, target, r.actor.AccessToken, body)
		require.Equal(t, r.status, status, "%s as %s: %s", row.key(), r.who, raw)
		require.Equal(t, r.code, mxCode([]byte(raw)), "%s as %s", row.key(), r.who)
		require.NotContains(t, raw, "Private matrix poll")
	}
	status, raw := w.do(row.method, foreign, actors.stranger.AccessToken, body)
	require.Equal(t, http.StatusNotFound, status, raw)
	require.Equal(t, "SCHEDULE_POLL.NOT_FOUND", mxCode([]byte(raw)), row.key())
	allowed := respondent.AccessToken
	if organizerOnly {
		allowed = w.owner()
	}
	status, raw = w.do(row.method, target, allowed, body)
	require.True(t, status >= 200 && status < 300, "%s: %d %s", row.key(), status, raw)
}

func mxCode(raw []byte) string {
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Code
}
