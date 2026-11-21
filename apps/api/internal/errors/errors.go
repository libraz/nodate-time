package apierrors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/libraz/nodate-time/apps/api/internal/blobref"
)

// Spec defines a reusable error specification.
type Spec struct {
	Status  int
	Code    string
	Message string
}

func (s *Spec) Error() string {
	return fmt.Sprintf("%s: %s", s.Code, s.Message)
}

// HumaError is the error body returned to clients. It carries the stable,
// machine-readable Code (in addition to the human Message) so the web client can
// branch on the failure type and localize it instead of string-matching the
// message. It implements huma.StatusError so Huma serializes it directly.
type HumaError struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Errors locates each field failure of a validation error; absent otherwise.
	Errors []*huma.ErrorDetail `json:"errors,omitempty"`
}

func (e *HumaError) Error() string  { return e.Message }
func (e *HumaError) GetStatus() int { return e.Status }

// ToHuma converts a Spec into a huma error response that includes the Code.
func ToHuma(s *Spec) error {
	return &HumaError{Status: s.Status, Code: s.Code, Message: s.Message}
}

// ensure huma's StatusError contract is satisfied at compile time.
var _ huma.StatusError = (*HumaError)(nil)

// --- Auth errors ---

var (
	AuthTokenMissing        = &Spec{Status: 401, Code: "AUTH.TOKEN_MISSING", Message: "Authorization header is required"}
	AuthTokenInvalid        = &Spec{Status: 401, Code: "AUTH.TOKEN_INVALID", Message: "Bearer token is invalid or expired"}
	AuthRegisterFailed      = &Spec{Status: 400, Code: "AUTH.REGISTER_FAILED", Message: "Unable to register with the supplied information"}
	AuthBadCredentials      = &Spec{Status: 401, Code: "AUTH.BAD_CREDENTIALS", Message: "Invalid email or password"}
	AuthWrongPassword       = &Spec{Status: 400, Code: "AUTH.WRONG_PASSWORD", Message: "Current password is incorrect"}
	AuthResetInvalid        = &Spec{Status: 400, Code: "AUTH.RESET_INVALID", Message: "Reset token is invalid or expired"}
	AuthVerificationInvalid = &Spec{Status: 400, Code: "AUTH.VERIFICATION_INVALID", Message: "Verification link is invalid or expired"}
	AuthOAuthFailed         = &Spec{Status: 400, Code: "AUTH.OAUTH_FAILED", Message: "OAuth authentication failed"}
	AuthSignupNotAllowed    = &Spec{Status: 403, Code: "AUTH.SIGNUP_NOT_ALLOWED", Message: "This email address is not permitted to sign up. Contact an administrator."}
	// AuthEmailUnsupported refuses an address the account columns cannot store.
	// Deliberately not SIGNUP_NOT_ALLOWED: that one tells the reader to ask an
	// administrator, and an administrator cannot help here -- the allow-list
	// column has the same charset as the one that refused the address.
	AuthEmailUnsupported = &Spec{Status: 400, Code: "AUTH.EMAIL_UNSUPPORTED", Message: "This email address uses characters this service cannot store"}
	AuthAdminRequired    = &Spec{Status: 403, Code: "AUTH.ADMIN_REQUIRED", Message: "Admin privileges required"}
	AuthSessionNotFound  = &Spec{Status: 404, Code: "AUTH.SESSION_NOT_FOUND", Message: "Session not found"}
	SecretsUnavailable   = &Spec{Status: 503, Code: "SECRETS.UNAVAILABLE", Message: "Secret encryption is not configured (set TC_SECRETS_KEY)"}
	// AuthMFARequired refuses a sign-in for an account that has confirmed a
	// second factor. This product has no way to ask for one, and letting the
	// password alone through would defeat the factor the account chose.
	AuthMFARequired = &Spec{Status: 403, Code: "AUTH.MFA_REQUIRED", Message: "This account requires a second factor that this app cannot verify"}
	// AuthLinkRefused refuses attaching an external identity to an existing
	// account by email alone, when that would let whoever controls the external
	// account take over one whose owner never proved the address.
	AuthLinkRefused = &Spec{Status: 409, Code: "AUTH.LINK_REFUSED", Message: "This email address belongs to an existing account that cannot be linked automatically"}
)

// --- Calendar errors ---

var (
	CalendarNotFound     = &Spec{Status: 404, Code: "CALENDAR.NOT_FOUND", Message: "Calendar not found"}
	CalendarAccessDenied = &Spec{Status: 403, Code: "CALENDAR.ACCESS_DENIED", Message: "You do not have access to this calendar"}
	CalendarRoleRequired = &Spec{Status: 403, Code: "CALENDAR.ROLE_REQUIRED", Message: "Insufficient role for this action"}
	// CalendarReadOnly refuses a change to a system calendar, whose contents
	// come from a provider feed, at any role.
	CalendarReadOnly       = &Spec{Status: 403, Code: "CALENDAR.READ_ONLY", Message: "This calendar is read-only"}
	CalendarCoverForbidden = &Spec{Status: 403, Code: "CALENDAR.COVER_FORBIDDEN", Message: "Uploaded covers are available only on shared personal calendars"}
	CalendarCoverTooLarge  = &Spec{Status: 413, Code: "CALENDAR.COVER_TOO_LARGE", Message: "Cover image exceeds the maximum size of 5 MB"}
	CalendarCoverInvalid   = &Spec{Status: 400, Code: "CALENDAR.COVER_INVALID", Message: "Only valid JPEG, PNG, and WebP cover images are accepted"}
	CalendarCoverConflict  = &Spec{Status: 409, Code: "CALENDAR.COVER_CONFLICT", Message: "The cover image conflicts with an unfinished upload"}
	CalendarStale          = &Spec{Status: 412, Code: "CALENDAR.STALE", Message: "This calendar changed since you opened it"}
	CalendarLimitReached   = &Spec{Status: 409, Code: "CALENDAR.LIMIT_REACHED", Message: "You have reached the maximum number of calendars"}
)

// --- Event errors ---

var (
	EventNotFound = &Spec{Status: 404, Code: "EVENT.NOT_FOUND", Message: "Event not found"}
	// EventEditDenied separates "you may write to this calendar" from "you may
	// change this particular event". Sharing a calendar is not the same as
	// handing everyone on it the right to rewrite each other's plans.
	EventEditDenied  = &Spec{Status: 403, Code: "EVENT.EDIT_DENIED", Message: "You cannot edit this event"}
	EventNotAttendee = &Spec{Status: 403, Code: "EVENT.NOT_ATTENDEE", Message: "You are not a participant of this event"}
	// EventStale reports that the event changed after the copy the caller's
	// If-Match names, so applying the write would silently undo whatever the
	// other writer just saved.
	EventStale = &Spec{Status: 412, Code: "EVENT.STALE", Message: "This event changed since you opened it"}
	// EventNotificationOffsetInvalid rejects a reminder offset outside the set
	// the clients can display, rather than storing one the event modal would
	// read back as no reminder at all.
	EventNotificationOffsetInvalid = &Spec{Status: 400, Code: "EVENT.NOTIFICATION_OFFSET_INVALID", Message: "Notification offset must be 0, 5, 10, 15, 30, 60, 120, 1440, or 2880 minutes"}
	// EventTaskOwned refuses a change to the synced columns of an event that a
	// task projects onto the calendar: the task owns them, and an edit made
	// here would be overwritten on the next sync.
	EventTaskOwned = &Spec{Status: 409, Code: "EVENT.TASK_OWNED", Message: "This event is managed by a task; change it there"}

	EventAllDayShape       = &Spec{Status: 400, Code: "EVENT.ALL_DAY_SHAPE", Message: "An all-day event takes dates only, with no time or time zone"}
	EventDateInvalid       = &Spec{Status: 400, Code: "EVENT.DATE_INVALID", Message: "The date is not a valid calendar date"}
	EventDateRangeInvalid  = &Spec{Status: 400, Code: "EVENT.DATE_RANGE_INVALID", Message: "The end of the event is before its start"}
	EventOccurrenceForm    = &Spec{Status: 400, Code: "EVENT.OCCURRENCE_FORM", Message: "The occurrence identifier is not in the form this series uses"}
	EventNotRecurring      = &Spec{Status: 400, Code: "EVENT.NOT_RECURRING", Message: "This event does not repeat"}
	EventDetailsHidden     = &Spec{Status: 403, Code: "EVENT.DETAILS_HIDDEN", Message: "The details of this event are not visible to you"}
	EventRestoreExpired    = &Spec{Status: 410, Code: "EVENT.RESTORE_EXPIRED", Message: "This event can no longer be restored"}
	EventNotDeleted        = &Spec{Status: 409, Code: "EVENT.NOT_DELETED", Message: "This event has not been deleted"}
	EventRestoreConflict   = &Spec{Status: 409, Code: "EVENT.RESTORE_CONFLICT", Message: "This event cannot be restored because something now occupies its place"}
	EventExceptionNotFound = &Spec{Status: 404, Code: "EVENT.EXCEPTION_NOT_FOUND", Message: "That occurrence has no change to undo"}
	SearchTooBroad         = &Spec{Status: 422, Code: "SEARCH.TOO_BROAD", Message: "The search covers too wide a range; narrow it"}
	ExportRangeTooLarge    = &Spec{Status: 422, Code: "EXPORT.RANGE_TOO_LARGE", Message: "The export range holds too many events; narrow it"}
)

// --- Comment errors ---

var (
	CommentNotFound     = &Spec{Status: 404, Code: "COMMENT.NOT_FOUND", Message: "Comment not found"}
	CommentAccessDenied = &Spec{Status: 403, Code: "COMMENT.ACCESS_DENIED", Message: "You can only edit your own comments"}
	CommentStale        = &Spec{Status: 412, Code: "COMMENT.STALE", Message: "This comment changed since you opened it"}
)

// --- Checklist errors ---

var (
	ChecklistItemNotFound = &Spec{Status: 404, Code: "CHECKLIST.NOT_FOUND", Message: "Checklist item not found"}
	ChecklistStale        = &Spec{Status: 412, Code: "CHECKLIST.STALE", Message: "This checklist item changed since you opened it"}
	ChecklistLimitReached = &Spec{Status: 409, Code: "CHECKLIST.LIMIT_REACHED", Message: "This event has the maximum number of checklist items"}
)

// --- Attachment errors ---

var (
	AttachmentNotFound = &Spec{Status: 404, Code: "ATTACHMENT.NOT_FOUND", Message: "Attachment not found"}
	AttachmentTooLarge = &Spec{Status: 413, Code: "ATTACHMENT.TOO_LARGE", Message: "File exceeds maximum size of 100MB"}
	// AttachmentDigestMismatch reports that the uploaded bytes are not the
	// ones the reservation declared.
	AttachmentDigestMismatch = &Spec{Status: 400, Code: "ATTACHMENT.DIGEST_MISMATCH", Message: "Uploaded file does not match the declared checksum"}
	AttachmentLimitReached   = &Spec{Status: 409, Code: "ATTACHMENT.LIMIT_REACHED", Message: "This event has the maximum number of attachments"}
	StorageUnavailable       = &Spec{Status: 503, Code: "STORAGE.UNAVAILABLE", Message: "File storage is not available"}
)

// --- Avatar errors ---

var (
	AvatarNotFound          = &Spec{Status: 404, Code: "AVATAR.NOT_FOUND", Message: "Avatar upload session not found"}
	AvatarTooLarge          = &Spec{Status: 413, Code: "AVATAR.TOO_LARGE", Message: "Avatar exceeds maximum size of 5MB"}
	AvatarUploadInvalid     = &Spec{Status: 400, Code: "AVATAR.UPLOAD_INVALID", Message: "Uploaded avatar does not match the declared file"}
	AvatarUploadLimit       = &Spec{Status: 429, Code: "AVATAR.UPLOAD_LIMIT", Message: "Too many active avatar uploads"}
	InvalidImageContentType = &Spec{Status: 400, Code: "IMAGE.INVALID_CONTENT_TYPE", Message: "Only JPEG, PNG, and WebP images are accepted"}
)

// --- Album errors ---

var (
	AlbumPhotoNotFound = &Spec{Status: 404, Code: "ALBUM.NOT_FOUND", Message: "Album photo not found"}
	AlbumStale         = &Spec{Status: 412, Code: "ALBUM.STALE", Message: "This photo changed since you opened it"}
	// AlbumInvalidContentType names the album's own accepted types, which
	// include GIF; the avatar's narrower list is InvalidImageContentType.
	AlbumInvalidContentType = &Spec{Status: 400, Code: "ALBUM.INVALID_CONTENT_TYPE", Message: "Only JPEG, PNG, WebP, and GIF images are accepted"}
	AlbumPhotoTooLarge      = &Spec{Status: 413, Code: "ALBUM.TOO_LARGE", Message: "Photo exceeds maximum size of 20MB"}
	// AlbumThumbnailTooLarge refuses the small rendering a photo is uploaded
	// with, not the photo. Deliberately not ALBUM.TOO_LARGE: that one names
	// the photo's own ceiling, so a rejected 1.5MB thumbnail would be
	// answered with a sentence about 20MB and send the reader looking at the
	// wrong file.
	AlbumThumbnailTooLarge = &Spec{Status: 413, Code: "ALBUM.THUMBNAIL_TOO_LARGE", Message: "Thumbnail exceeds maximum size of 1MB"}
	// AlbumThumbnailIncomplete answers a request that names a thumbnail's
	// type without its size, or the reverse. Answering it as "no thumbnail"
	// would be silent: the caller believes it is sending one, and the only
	// symptom is a grid that keeps downloading full-size pictures.
	AlbumThumbnailIncomplete = &Spec{Status: 400, Code: "ALBUM.THUMBNAIL_INCOMPLETE", Message: "A thumbnail needs both its content type and its size"}
)

// --- Member errors ---

var (
	MemberNotFound     = &Spec{Status: 404, Code: "MEMBER.NOT_FOUND", Message: "Member not found"}
	MemberLastAdmin    = &Spec{Status: 400, Code: "MEMBER.LAST_ADMIN", Message: "Cannot remove the last admin"}
	MemberSelfModify   = &Spec{Status: 400, Code: "MEMBER.SELF_MODIFY", Message: "You cannot change your own membership"}
	MemberAlreadyAdded = &Spec{Status: 409, Code: "MEMBER.ALREADY_ADDED", Message: "This person is already a member of the calendar"}
	MemberLimitReached = &Spec{Status: 409, Code: "MEMBER.LIMIT_REACHED", Message: "This calendar has the maximum number of members"}
)

// --- Instance admin errors ---

var (
	// AdminSelfRevoke refuses an administrator taking away their own rights.
	// Stepping down is asked of another administrator, so one careless click
	// cannot be the thing that leaves an instance with nobody to run it.
	AdminSelfRevoke = &Spec{Status: 400, Code: "ADMIN.SELF_REVOKE", Message: "You cannot revoke your own instance administrator rights"}
	// AdminLastInstanceAdmin refuses the revocation that would leave the
	// instance with no administrator at all. Granting the rights back is not
	// an API operation, so recovering from it means a hand-written statement
	// against the database -- the thing this endpoint exists to avoid.
	AdminLastInstanceAdmin = &Spec{Status: 400, Code: "ADMIN.LAST_INSTANCE_ADMIN", Message: "Cannot revoke the last instance administrator"}

	AdminAlreadyAdmin              = &Spec{Status: 409, Code: "ADMIN.ALREADY_ADMIN", Message: "This user is already an instance administrator"}
	AdminUserNotFound              = &Spec{Status: 404, Code: "ADMIN.USER_NOT_FOUND", Message: "User not found"}
	AdminAdminsLimitReached        = &Spec{Status: 409, Code: "ADMIN.ADMINS_LIMIT_REACHED", Message: "The instance has the maximum number of administrators"}
	AdminAllowedEmailsLimitReached = &Spec{Status: 409, Code: "ADMIN.ALLOWED_EMAILS_LIMIT_REACHED", Message: "The allow-list has the maximum number of addresses"}
)

// --- Invite errors ---

var (
	InviteNotFound     = &Spec{Status: 404, Code: "INVITE.NOT_FOUND", Message: "Invite not found or expired"}
	InviteExpired      = &Spec{Status: 410, Code: "INVITE.EXPIRED", Message: "Invite has expired or reached max uses"}
	InviteLimitReached = &Spec{Status: 409, Code: "INVITE.LIMIT_REACHED", Message: "This calendar has the maximum number of invite links"}
	// InvitePublicViewOnly rejects joining through a public, unlimited viewer link;
	// such links exist only for read-only embedding, not for claiming membership.
	InvitePublicViewOnly      = &Spec{Status: 403, Code: "INVITE.PUBLIC_VIEW_ONLY", Message: "This is a public view-only link and cannot be joined"}
	InvitePublicAlreadyExists = &Spec{Status: 409, Code: "INVITE.PUBLIC_ALREADY_EXISTS", Message: "An active public link already exists for this calendar"}
	// InviteCalendarGone answers a link whose calendar has since been deleted.
	// Nothing is wrong with the link, so saying it is invalid sends the holder
	// back to ask for another one that cannot exist either; what they need to
	// know is that the other end of it is gone.
	InviteCalendarGone = &Spec{Status: 410, Code: "INVITE.CALENDAR_GONE", Message: "The calendar this link points to no longer exists"}
)

// --- Memo errors ---

var (
	MemoNotFound = &Spec{Status: 404, Code: "MEMO.NOT_FOUND", Message: "Memo not found"}
	MemoStale    = &Spec{Status: 412, Code: "MEMO.STALE", Message: "This memo changed since you opened it"}
)

// --- Schedule poll errors ---

var (
	SchedulePollNotFound             = &Spec{Status: 404, Code: "SCHEDULE_POLL.NOT_FOUND", Message: "Schedule poll not found"}
	SchedulePollAccessDenied         = &Spec{Status: 403, Code: "SCHEDULE_POLL.ACCESS_DENIED", Message: "You are not a recipient or manager of this schedule poll"}
	SchedulePollStale                = &Spec{Status: 412, Code: "SCHEDULE_POLL.STALE", Message: "This schedule poll changed; reload it before continuing"}
	SchedulePollResponseStale        = &Spec{Status: 412, Code: "SCHEDULE_POLL.RESPONSE_STALE", Message: "Your answer changed since you opened it; reload it before continuing"}
	SchedulePollConflict             = &Spec{Status: 409, Code: "SCHEDULE_POLL.CONFLICT", Message: "This schedule poll cannot be changed in its current state"}
	SchedulePollSlotUnavailable      = &Spec{Status: 409, Code: "SCHEDULE_POLL.SLOT_UNAVAILABLE", Message: "The selected time now conflicts with a fixed event"}
	SchedulePollConfirmationRequired = &Spec{Status: 409, Code: "SCHEDULE_POLL.CONFIRMATION_REQUIRED", Message: "Confirm the selected availability warnings before finalizing"}
	SchedulePollTooManyMembers       = &Spec{Status: 422, Code: "SCHEDULE_POLL.TOO_MANY_MEMBERS", Message: "This calendar has too many members for one schedule poll"}
	SchedulePollTooManySlots         = &Spec{Status: 422, Code: "SCHEDULE_POLL.TOO_MANY_SLOTS", Message: "This schedule poll has too many candidate times"}
	SchedulePollTooMuchHistory       = &Spec{Status: 422, Code: "SCHEDULE_POLL.HISTORY_LIMIT", Message: "This calendar has too many schedule polls for one history response"}
)

// --- Upload errors ---

var (
	// UploadDigestMismatch covers both a malformed declared digest and bytes
	// that do not hash to the declared one.
	UploadDigestMismatch = &Spec{Status: 422, Code: "UPLOAD.DIGEST_MISMATCH", Message: "Uploaded file does not match the declared checksum"}
	UploadTooLarge       = &Spec{Status: 413, Code: "UPLOAD.TOO_LARGE", Message: "Uploaded file exceeds the size it was reserved for"}
)

// --- General errors ---

var (
	InternalUnexpected = &Spec{Status: http.StatusInternalServerError, Code: "INTERNAL.UNEXPECTED", Message: "An unexpected error occurred"}
	BadRequest         = &Spec{Status: http.StatusBadRequest, Code: "REQUEST.INVALID", Message: "Invalid request"}
	NotFound           = &Spec{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "Resource not found"}
	Conflict           = &Spec{Status: http.StatusConflict, Code: "CONFLICT", Message: "Resource already exists"}
	RateLimited        = &Spec{Status: http.StatusTooManyRequests, Code: "RATE.LIMITED", Message: "Too many requests, please try again later"}

	PreconditionRequired = &Spec{Status: http.StatusPreconditionRequired, Code: "REQUEST.PRECONDITION_REQUIRED", Message: "This change must name the version it replaces in an If-Match header"}
	BadIfMatch           = &Spec{Status: http.StatusBadRequest, Code: "REQUEST.BAD_IF_MATCH", Message: "The If-Match header is not a valid entity tag"}
	Validation           = &Spec{Status: http.StatusUnprocessableEntity, Code: "REQUEST.VALIDATION", Message: "The request failed validation"}
)

// WriteSpec renders a Spec straight onto a ResponseWriter, for the layers that
// run outside Huma's error handling — middleware and the panic recovery — so a
// client sees the same envelope no matter where the failure came from. Handlers
// inside Huma use ToHuma instead.
func WriteSpec(w http.ResponseWriter, s *Spec) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.Status)
	// Encoding cannot fail for this shape, and the status line is already
	// written, so there is nothing useful to do with an error here.
	_ = json.NewEncoder(w).Encode(HumaError{Status: s.Status, Code: s.Code, Message: s.Message})
}

// FromBlobref maps a failed verification of uploaded bytes to its Spec. The
// second result is false for any other error.
func FromBlobref(err error) (*Spec, bool) {
	switch {
	case errors.Is(err, blobref.ErrDigestMismatch), errors.Is(err, blobref.ErrBadDigest):
		return UploadDigestMismatch, true
	case errors.Is(err, blobref.ErrTooLarge):
		return UploadTooLarge, true
	}
	return nil, false
}
