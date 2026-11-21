package events

import (
	"time"
)

// Attachments

type AttachmentResponse struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"contentType"`
	ByteSize    int64     `json:"byteSize"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PresignUploadInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Filename    string `json:"filename" minLength:"1" maxLength:"500"`
		ContentType string `json:"contentType,omitempty" maxLength:"255" required:"false"`
		// ByteSize is bound into the upload URL's signature, so it is also the
		// size limit of the upload itself. A declared zero cannot be bound and
		// would leave the URL accepting a body of any size.
		ByteSize int64 `json:"byteSize" minimum:"1" doc:"size of the file in bytes; the upload is signed for exactly this length"`
		// SHA256 is the hex digest of the bytes about to be uploaded. The
		// blob is content-addressed, so the same file attached twice is
		// stored once; the digest is also what the storage key is built
		// from, which keeps the client's filename out of the key entirely.
		SHA256 string `json:"sha256" minLength:"64" maxLength:"64" pattern:"^[0-9a-fA-F]{64}$" doc:"lowercase hex SHA-256 of the file contents"`
	}
}
type PresignUploadOutput struct {
	Body struct {
		AttachmentID string `json:"attachmentId"`
		UploadURL    string `json:"uploadUrl" doc:"Signed PUT URL, valid for 15 minutes. Empty when these bytes are already stored where the caller can read them: skip the upload and confirm."`
	}
}

type ConfirmAttachmentInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type ConfirmAttachmentOutput struct {
	Body AttachmentResponse
}

type ListAttachmentsInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
}
type ListAttachmentsOutput struct {
	Body []AttachmentResponse
}

type GetAttachmentDownloadInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type GetAttachmentDownloadOutput struct {
	Body struct {
		DownloadURL string `json:"downloadUrl"`
	}
}

type DeleteAttachmentInput struct {
	CalendarID   string `path:"calendarId"`
	EventID      string `path:"eventId"`
	AttachmentID string `path:"attachmentId"`
}
type DeleteAttachmentOutput struct{}
