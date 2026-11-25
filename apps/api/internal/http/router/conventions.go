package router

import "net/http"

// writeGuard says how an operation is protected against a stale write.
type writeGuard int

const (
	// guardRead marks an operation that changes nothing.
	guardRead writeGuard = iota
	// guardConditional marks a write that requires If-Match, answers 412 and
	// 428, and returns the new etag.
	guardConditional
	// guardUnconditional marks a write that takes no If-Match; the reason says
	// why a stale caller cannot do harm.
	guardUnconditional
)

// listShape is the envelope a listing answers with.
type listShape int

const (
	listNone listShape = iota
	// listCursor is {items, nextCursor}: a listing that can be walked.
	listCursor
	// listWindow is {items, hasMore}: a listing cut by range or count.
	listWindow
	// listArray is a bare array, bounded by a limit enforced at write time.
	listArray
)

// convention is the contract one operation is held to.
type convention struct {
	ID     string
	Method string
	Path   string
	// Status is the success status the operation answers with.
	Status int
	Guard  writeGuard
	// Reason is required exactly when Guard is guardUnconditional.
	Reason string
	List   listShape
}

const (
	reasonNewRow    = "creates a new row"
	reasonByID      = "acts on one row by identity; nothing is merged"
	reasonSingle    = "replaces a single value"
	reasonOwn       = "acts on the caller's own state"
	reasonExchange  = "exchanges a token or credential"
	reasonQuery     = "reads, but is addressed with POST"
	reasonUnused    = "the resource carries no version"
	reasonIssueOnce = "issues a credential once; repeats return the same one"
)

func read(id, path string, list listShape) convention {
	return convention{ID: id, Method: http.MethodGet, Path: path, Status: http.StatusOK, List: list}
}

func create(id, path string) convention {
	return convention{ID: id, Method: http.MethodPost, Path: path, Status: http.StatusCreated, Guard: guardUnconditional, Reason: reasonNewRow}
}

func action(id, path string, status int, reason string) convention {
	return convention{ID: id, Method: http.MethodPost, Path: path, Status: status, Guard: guardUnconditional, Reason: reason}
}

func actionIf(id, path string) convention {
	return convention{ID: id, Method: http.MethodPost, Path: path, Status: http.StatusOK, Guard: guardConditional}
}

func put(id, path, reason string) convention {
	return convention{ID: id, Method: http.MethodPut, Path: path, Status: http.StatusOK, Guard: guardUnconditional, Reason: reason}
}

func putIf(id, path string) convention {
	return convention{ID: id, Method: http.MethodPut, Path: path, Status: http.StatusOK, Guard: guardConditional}
}

func patch(id, path string) convention {
	return convention{ID: id, Method: http.MethodPatch, Path: path, Status: http.StatusOK, Guard: guardConditional}
}

func patchUnguarded(id, path, reason string) convention {
	return convention{ID: id, Method: http.MethodPatch, Path: path, Status: http.StatusOK, Guard: guardUnconditional, Reason: reason}
}

func remove(id, path string) convention {
	return convention{ID: id, Method: http.MethodDelete, Path: path, Status: http.StatusNoContent, Guard: guardConditional}
}

func removeUnguarded(id, path, reason string) convention {
	return convention{ID: id, Method: http.MethodDelete, Path: path, Status: http.StatusNoContent, Guard: guardUnconditional, Reason: reason}
}

const (
	calPath   = "/calendars/{calendarId}"
	eventPath = calPath + "/events/{eventId}"
	pollPath  = calPath + "/schedule-polls/{pollId}"
)

// operationConventions lists every operation the API documents.
var operationConventions = []convention{
	// Admin
	read("list-allowed-emails", "/admin/allowed-emails", listNone),
	create("create-allowed-email", "/admin/allowed-emails"),
	removeUnguarded("delete-allowed-email", "/admin/allowed-emails/{emailId}", reasonByID),
	read("list-instance-admins", "/admin/instance-admins", listArray),
	create("add-instance-admin", "/admin/instance-admins"),
	removeUnguarded("revoke-instance-admin", "/admin/instance-admins/{userId}", reasonByID),
	read("list-oauth-providers", "/admin/oauth-providers", listNone),
	removeUnguarded("delete-oauth-provider", "/admin/oauth-providers/{provider}", reasonByID),
	patchUnguarded("update-oauth-provider", "/admin/oauth-providers/{provider}", reasonUnused),

	// Auth
	{ID: "get-app-password-profile", Method: http.MethodGet, Path: "/auth/app-password-profiles/{ticket}", Status: http.StatusOK, Guard: guardUnconditional, Reason: reasonIssueOnce},
	action("login", "/auth/login", http.StatusOK, reasonExchange),
	action("logout", "/auth/logout", http.StatusNoContent, reasonOwn),
	read("list-oauth-providers-public", "/auth/oauth/providers", listNone),
	{ID: "oauth-callback", Method: http.MethodGet, Path: "/auth/oauth/{provider}/callback", Status: http.StatusNoContent},
	read("oauth-start", "/auth/oauth/{provider}/start", listNone),
	action("confirm-password-reset", "/auth/password-reset/confirm", http.StatusOK, reasonExchange),
	action("request-password-reset", "/auth/password-reset/request", http.StatusOK, reasonOwn),
	action("refresh-session", "/auth/refresh", http.StatusOK, reasonExchange),
	create("register", "/auth/register"),
	action("confirm-email-verification", "/auth/verify-email/confirm", http.StatusOK, reasonExchange),

	// Calendars
	read("list-calendars", "/calendars", listArray),
	create("create-calendar", "/calendars"),
	remove("delete-calendar", calPath),
	read("get-calendar", calPath, listNone),
	patch("update-calendar", calPath),
	read("get-calendar-activity", calPath+"/activity", listCursor),
	read("export-events", calPath+"/export", listNone),
	action("import-events", calPath+"/import", http.StatusOK, reasonNewRow),
	put("confirm-calendar-cover", calPath+"/cover", reasonUnused),
	create("presign-calendar-cover", calPath+"/cover/presign"),
	removeUnguarded("delete-calendar-cover", calPath+"/cover", reasonUnused),
	read("list-labels", calPath+"/labels", listArray),
	create("create-label", calPath+"/labels"),
	patch("update-label", calPath+"/labels/{labelId}"),
	remove("delete-label", calPath+"/labels/{labelId}"),

	// Members
	read("list-member-candidates", calPath+"/member-candidates", listWindow),
	read("list-members", calPath+"/members", listArray),
	create("add-member", calPath+"/members"),
	removeUnguarded("remove-member", calPath+"/members/{userId}", reasonByID),
	put("update-member-color", calPath+"/members/{userId}/color", reasonSingle),
	put("update-member-role", calPath+"/members/{userId}/role", reasonSingle),

	// Albums
	read("list-album-photos", calPath+"/albums", listCursor),
	create("presign-album-photo", calPath+"/albums/presign"),
	remove("delete-album-photo", calPath+"/albums/{photoId}"),
	patch("update-album-photo", calPath+"/albums/{photoId}"),
	action("confirm-album-photo", calPath+"/albums/{photoId}/confirm", http.StatusOK, reasonByID),
	read("get-album-photo-download", calPath+"/albums/{photoId}/download", listNone),

	// Events
	read("list-events", calPath+"/events", listWindow),
	create("create-event", calPath+"/events"),
	remove("delete-event", eventPath),
	read("get-event", eventPath, listNone),
	patch("update-event", eventPath),
	put("set-event-attendee-can-edit", eventPath+"/attendees/{userId}", reasonSingle),
	put("set-event-rsvp", eventPath+"/rsvp", reasonSingle),
	read("get-event-history", eventPath+"/history", listCursor),

	// Comments
	read("list-comments", eventPath+"/activities", listCursor),
	create("create-comment", eventPath+"/activities"),
	remove("delete-comment", eventPath+"/activities/{commentId}"),
	patch("update-comment", eventPath+"/activities/{commentId}"),

	// Attachments
	read("list-attachments", eventPath+"/attachments", listArray),
	create("presign-upload", eventPath+"/attachments/presign"),
	removeUnguarded("delete-attachment", eventPath+"/attachments/{attachmentId}", reasonByID),
	action("confirm-attachment", eventPath+"/attachments/{attachmentId}/confirm", http.StatusOK, reasonByID),
	read("get-attachment-download", eventPath+"/attachments/{attachmentId}/download", listNone),

	// Checklist
	read("list-checklist-items", eventPath+"/checklist", listArray),
	create("create-checklist-item", eventPath+"/checklist"),
	remove("delete-checklist-item", eventPath+"/checklist/{itemId}"),
	patch("update-checklist-item", eventPath+"/checklist/{itemId}"),

	// Invites
	read("list-invites", calPath+"/invites", listArray),
	create("create-invite", calPath+"/invites"),
	removeUnguarded("delete-invite", calPath+"/invites/{inviteId}", reasonByID),
	action("accept-invite", "/invites/{token}/accept", http.StatusOK, reasonOwn),

	// Memos
	read("list-memos", calPath+"/memos", listNone),
	create("create-memo", calPath+"/memos"),
	remove("delete-memo", calPath+"/memos/{memoId}"),
	patch("update-memo", calPath+"/memos/{memoId}"),
	read("get-memo-history", calPath+"/memos/{memoId}/history", listNone),

	// Schedule polls
	read("list-schedule-polls", calPath+"/schedule-polls", listNone),
	create("create-schedule-poll", calPath+"/schedule-polls"),
	action("search-schedule-availability", calPath+"/schedule-polls/availability", http.StatusOK, reasonQuery),
	read("get-schedule-availability-sharing", calPath+"/schedule-polls/availability-sharing", listNone),
	put("set-schedule-availability-sharing", calPath+"/schedule-polls/availability-sharing", reasonSingle),
	read("get-schedule-poll", pollPath, listNone),
	patch("update-schedule-poll", pollPath),
	actionIf("cancel-schedule-poll", pollPath+"/cancel"),
	actionIf("finalize-schedule-poll", pollPath+"/finalize"),
	action("refresh-schedule-poll-availability", pollPath+"/refresh-availability", http.StatusOK, reasonQuery),
	read("get-schedule-poll-response", pollPath+"/response", listNone),
	putIf("set-schedule-poll-response", pollPath+"/response"),
	read("list-my-schedule-poll-actions", "/user/schedule-poll-actions", listNone),

	// Share
	read("public-calendar", "/share/{token}", listNone),
	read("public-events", "/share/{token}/events", listWindow),

	// User
	read("get-me", "/user", listNone),
	patchUnguarded("update-user", "/user", reasonOwn),
	removeUnguarded("delete-avatar", "/user/avatar", reasonOwn),
	put("confirm-avatar", "/user/avatar", reasonOwn),
	create("presign-avatar", "/user/avatar/presign"),
	put("change-password", "/user/password", reasonOwn),
	read("list-sessions", "/user/sessions", listArray),
	removeUnguarded("revoke-session", "/user/sessions/{sessionId}", reasonOwn),
	action("resend-email-verification", "/user/verify-email/resend", http.StatusOK, reasonOwn),
	read("list-app-passwords", "/user/app-passwords", listArray),
	create("create-app-password", "/user/app-passwords"),
	action("create-app-password-profile-link", "/user/app-passwords/profile-links", http.StatusOK, reasonExchange),
	removeUnguarded("revoke-app-password", "/user/app-passwords/{appPasswordId}", reasonOwn),
}
