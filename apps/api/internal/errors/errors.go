package apierrors

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
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
	AuthEmailExists         = &Spec{Status: 409, Code: "AUTH.EMAIL_EXISTS", Message: "Email address is already registered"}
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
)

// --- Calendar errors ---

var (
	CalendarNotFound     = &Spec{Status: 404, Code: "CALENDAR.NOT_FOUND", Message: "Calendar not found"}
	CalendarAccessDenied = &Spec{Status: 403, Code: "CALENDAR.ACCESS_DENIED", Message: "You do not have access to this calendar"}
	CalendarRoleRequired = &Spec{Status: 403, Code: "CALENDAR.ROLE_REQUIRED", Message: "Insufficient role for this action"}
)

// --- Event errors ---

var (
	EventNotFound     = &Spec{Status: 404, Code: "EVENT.NOT_FOUND", Message: "Event not found"}
	EventAccessDenied = &Spec{Status: 403, Code: "EVENT.ACCESS_DENIED", Message: "You do not have access to this event"}
	// EventEditDenied separates "you may write to this calendar" from "you may
	// change this particular event". Sharing a calendar is not the same as
	// handing everyone on it the right to rewrite each other's plans.
	EventEditDenied  = &Spec{Status: 403, Code: "EVENT.EDIT_DENIED", Message: "You cannot edit this event"}
	EventNotAttendee = &Spec{Status: 403, Code: "EVENT.NOT_ATTENDEE", Message: "You are not a participant of this event"}
	// EventStale reports that the event changed after the copy the caller is
	// replacing. An update carries the whole event, so applying it anyway
	// would silently undo whatever the other writer just saved.
	EventStale = &Spec{Status: 409, Code: "EVENT.STALE", Message: "This event changed since you opened it"}
	// EventNotificationOffsetInvalid rejects a reminder offset outside the set
	// the clients can display, rather than storing one the event modal would
	// read back as no reminder at all.
	EventNotificationOffsetInvalid = &Spec{Status: 400, Code: "EVENT.NOTIFICATION_OFFSET_INVALID", Message: "Notification offset must be 0, 5, 10, 15, 30, 60, 120, 1440, or 2880 minutes"}
)

// --- Comment errors ---

var (
	CommentNotFound     = &Spec{Status: 404, Code: "COMMENT.NOT_FOUND", Message: "Comment not found"}
	CommentAccessDenied = &Spec{Status: 403, Code: "COMMENT.ACCESS_DENIED", Message: "You can only edit your own comments"}
)

// --- Checklist errors ---

var (
	ChecklistItemNotFound = &Spec{Status: 404, Code: "CHECKLIST.NOT_FOUND", Message: "Checklist item not found"}
)

// --- Attachment errors ---

var (
	AttachmentNotFound = &Spec{Status: 404, Code: "ATTACHMENT.NOT_FOUND", Message: "Attachment not found"}
	AttachmentTooLarge = &Spec{Status: 400, Code: "ATTACHMENT.TOO_LARGE", Message: "File exceeds maximum size of 100MB"}
	// AttachmentDigestMismatch reports that the uploaded bytes are not the
	// ones the reservation declared.
	AttachmentDigestMismatch = &Spec{Status: 400, Code: "ATTACHMENT.DIGEST_MISMATCH", Message: "Uploaded file does not match the declared checksum"}
	StorageUnavailable       = &Spec{Status: 503, Code: "STORAGE.UNAVAILABLE", Message: "File storage is not available"}
)

// --- Avatar errors ---

var (
	AvatarNotFound          = &Spec{Status: 404, Code: "AVATAR.NOT_FOUND", Message: "Avatar upload session not found"}
	AvatarTooLarge          = &Spec{Status: 400, Code: "AVATAR.TOO_LARGE", Message: "Avatar exceeds maximum size of 5MB"}
	AvatarUploadInvalid     = &Spec{Status: 400, Code: "AVATAR.UPLOAD_INVALID", Message: "Uploaded avatar does not match the declared file"}
	AvatarUploadLimit       = &Spec{Status: 429, Code: "AVATAR.UPLOAD_LIMIT", Message: "Too many active avatar uploads"}
	InvalidImageContentType = &Spec{Status: 400, Code: "IMAGE.INVALID_CONTENT_TYPE", Message: "Only JPEG, PNG, and WebP images are accepted"}
)

// --- Album errors ---

var (
	AlbumPhotoNotFound = &Spec{Status: 404, Code: "ALBUM.NOT_FOUND", Message: "Album photo not found"}
	AlbumPhotoTooLarge = &Spec{Status: 400, Code: "ALBUM.TOO_LARGE", Message: "Photo exceeds maximum size of 20MB"}
	// AlbumThumbnailTooLarge refuses the small rendering a photo is uploaded
	// with, not the photo. Deliberately not ALBUM.TOO_LARGE: that one names
	// the photo's own ceiling, so a rejected 1.5MB thumbnail would be
	// answered with a sentence about 20MB and send the reader looking at the
	// wrong file.
	AlbumThumbnailTooLarge = &Spec{Status: 400, Code: "ALBUM.THUMBNAIL_TOO_LARGE", Message: "Thumbnail exceeds maximum size of 1MB"}
	// AlbumThumbnailIncomplete answers a request that names a thumbnail's
	// type without its size, or the reverse. Answering it as "no thumbnail"
	// would be silent: the caller believes it is sending one, and the only
	// symptom is a grid that keeps downloading full-size pictures.
	AlbumThumbnailIncomplete = &Spec{Status: 400, Code: "ALBUM.THUMBNAIL_INCOMPLETE", Message: "A thumbnail needs both its content type and its size"}
)

// --- Member errors ---

var (
	MemberNotFound      = &Spec{Status: 404, Code: "MEMBER.NOT_FOUND", Message: "Member not found"}
	MemberAlreadyExists = &Spec{Status: 409, Code: "MEMBER.ALREADY_EXISTS", Message: "User is already a member of this calendar"}
	MemberLastAdmin     = &Spec{Status: 400, Code: "MEMBER.LAST_ADMIN", Message: "Cannot remove the last admin"}
	MemberSelfModify    = &Spec{Status: 400, Code: "MEMBER.SELF_MODIFY", Message: "You cannot change your own membership"}
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
)

// --- Invite errors ---

var (
	InviteNotFound = &Spec{Status: 404, Code: "INVITE.NOT_FOUND", Message: "Invite not found or expired"}
	InviteExpired  = &Spec{Status: 410, Code: "INVITE.EXPIRED", Message: "Invite has expired or reached max uses"}
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
)

// --- General errors ---

var (
	InternalUnexpected = &Spec{Status: http.StatusInternalServerError, Code: "INTERNAL.UNEXPECTED", Message: "An unexpected error occurred"}
	BadRequest         = &Spec{Status: http.StatusBadRequest, Code: "REQUEST.INVALID", Message: "Invalid request"}
	NotFound           = &Spec{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "Resource not found"}
	Conflict           = &Spec{Status: http.StatusConflict, Code: "CONFLICT", Message: "Resource already exists"}
	RateLimited        = &Spec{Status: http.StatusTooManyRequests, Code: "RATE.LIMITED", Message: "Too many requests, please try again later"}
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
