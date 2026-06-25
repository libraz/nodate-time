package apierrors

import (
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
	AuthTokenMissing     = &Spec{Status: 401, Code: "AUTH.TOKEN_MISSING", Message: "Authorization header is required"}
	AuthTokenInvalid     = &Spec{Status: 401, Code: "AUTH.TOKEN_INVALID", Message: "Bearer token is invalid or expired"}
	AuthEmailExists      = &Spec{Status: 409, Code: "AUTH.EMAIL_EXISTS", Message: "Email address is already registered"}
	AuthBadCredentials   = &Spec{Status: 401, Code: "AUTH.BAD_CREDENTIALS", Message: "Invalid email or password"}
	AuthWrongPassword    = &Spec{Status: 400, Code: "AUTH.WRONG_PASSWORD", Message: "Current password is incorrect"}
	AuthResetInvalid     = &Spec{Status: 400, Code: "AUTH.RESET_INVALID", Message: "Reset token is invalid or expired"}
	AuthOAuthFailed      = &Spec{Status: 400, Code: "AUTH.OAUTH_FAILED", Message: "OAuth authentication failed"}
	AuthSignupNotAllowed = &Spec{Status: 403, Code: "AUTH.SIGNUP_NOT_ALLOWED", Message: "This email address is not permitted to sign up. Contact an administrator."}
	AuthAdminRequired    = &Spec{Status: 403, Code: "AUTH.ADMIN_REQUIRED", Message: "Admin privileges required"}
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
	StorageUnavailable = &Spec{Status: 503, Code: "STORAGE.UNAVAILABLE", Message: "File storage is not available"}
)

// --- Avatar errors ---

var (
	AvatarNotFound          = &Spec{Status: 404, Code: "AVATAR.NOT_FOUND", Message: "Avatar upload session not found"}
	AvatarTooLarge          = &Spec{Status: 400, Code: "AVATAR.TOO_LARGE", Message: "Avatar exceeds maximum size of 5MB"}
	InvalidImageContentType = &Spec{Status: 400, Code: "IMAGE.INVALID_CONTENT_TYPE", Message: "Only JPEG, PNG, and WebP images are accepted"}
)

// --- Album errors ---

var (
	AlbumPhotoNotFound = &Spec{Status: 404, Code: "ALBUM.NOT_FOUND", Message: "Album photo not found"}
	AlbumPhotoTooLarge = &Spec{Status: 400, Code: "ALBUM.TOO_LARGE", Message: "Photo exceeds maximum size of 20MB"}
)

// --- Member errors ---

var (
	MemberNotFound      = &Spec{Status: 404, Code: "MEMBER.NOT_FOUND", Message: "Member not found"}
	MemberAlreadyExists = &Spec{Status: 409, Code: "MEMBER.ALREADY_EXISTS", Message: "User is already a member of this calendar"}
	MemberLastAdmin     = &Spec{Status: 400, Code: "MEMBER.LAST_ADMIN", Message: "Cannot remove the last admin"}
	MemberSelfModify    = &Spec{Status: 400, Code: "MEMBER.SELF_MODIFY", Message: "You cannot change your own membership"}
)

// --- Invite errors ---

var (
	InviteNotFound = &Spec{Status: 404, Code: "INVITE.NOT_FOUND", Message: "Invite not found or expired"}
	InviteExpired  = &Spec{Status: 410, Code: "INVITE.EXPIRED", Message: "Invite has expired or reached max uses"}
	// InvitePublicViewOnly rejects joining through a public, unlimited viewer link;
	// such links exist only for read-only embedding, not for claiming membership.
	InvitePublicViewOnly = &Spec{Status: 403, Code: "INVITE.PUBLIC_VIEW_ONLY", Message: "This is a public view-only link and cannot be joined"}
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
)
