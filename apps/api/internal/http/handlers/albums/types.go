package albums

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

type AlbumPhotoResponse struct {
	ID          string `json:"id"`
	CalendarID  string `json:"calendarId"`
	Caption     string `json:"caption"`
	ContentType string `json:"contentType"`
	ByteSize    int64  `json:"byteSize"`
	Width       *int   `json:"width,omitempty"`
	Height      *int   `json:"height,omitempty"`
	// EventID is null when the photo has no event link, and also when the
	// reader may not see the linked event.
	EventID *string `json:"eventId" nullable:"true"`
	// ETag names this revision of the photo; send it as If-Match to change or delete it.
	ETag              string    `json:"etag"`
	TakenAt           time.Time `json:"takenAt"`
	CreatedAt         time.Time `json:"createdAt"`
	UploaderID        string    `json:"uploaderId"`
	UploaderName      string    `json:"uploaderName"`
	UploaderAvatarURL string    `json:"uploaderAvatarUrl,omitempty"`
	ImageURL          string    `json:"imageUrl"`
	// ThumbnailURL is the grid-sized rendering, absent when the photo has
	// none. A caller that wants a small picture asks for this and falls back
	// to ImageURL; one that wants the picture itself -- a lightbox, a
	// download -- uses ImageURL and never this.
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
}

type ListPhotosInput struct {
	CalendarID string `path:"calendarId"`
	Cursor     string `query:"cursor" required:"false" doc:"Opaque cursor from a previous response"`
	Limit      int    `query:"limit" required:"false" minimum:"1" maximum:"100" default:"30"`
}

type ListPhotosBody struct {
	Items      []AlbumPhotoResponse `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

type ListPhotosOutput struct {
	Body ListPhotosBody
}

type PresignPhotoBody struct {
	ContentType string    `json:"contentType" doc:"MIME type, must be image/*"`
	ByteSize    int64     `json:"byteSize" minimum:"1"`
	SHA256      string    `json:"sha256" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"lowercase hex SHA-256 of the photo bytes"`
	Caption     string    `json:"caption" required:"false" maxLength:"500"`
	EventID     string    `json:"eventId" required:"false" doc:"Optional event public ID"`
	TakenAt     time.Time `json:"takenAt" required:"false" doc:"EXIF capture time. Defaults to upload time."`
	Width       int       `json:"width" required:"false" minimum:"1"`
	Height      int       `json:"height" required:"false" minimum:"1"`
	// ThumbnailContentType and ThumbnailByteSize declare a second, smaller
	// rendering the caller intends to upload. Both absent means it is not
	// sending one, which is a normal outcome rather than an error: an
	// animated GIF has no still thumbnail worth having, and a photo already
	// smaller than a tile gains nothing from one.
	ThumbnailContentType string `json:"thumbnailContentType" required:"false" doc:"MIME type of the thumbnail, must be image/*"`
	ThumbnailByteSize    int64  `json:"thumbnailByteSize" required:"false" minimum:"1"`
	// ThumbnailSHA256 is required exactly when a thumbnail is declared.
	ThumbnailSHA256 string `json:"thumbnailSha256" required:"false" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"lowercase hex SHA-256 of the thumbnail bytes"`
}

type PresignPhotoInput struct {
	CalendarID string `path:"calendarId"`
	Body       PresignPhotoBody
}

type PresignPhotoResult struct {
	PhotoID   string `json:"photoId"`
	UploadURL string `json:"uploadUrl"`
	// ThumbnailUploadURL is issued only when the request declared a
	// thumbnail. Uploading to it is optional even then: the confirm looks for
	// the object and carries on without it, because a photo that reached the
	// server must not be lost to a second request that did not.
	ThumbnailUploadURL string `json:"thumbnailUploadUrl,omitempty"`
}

type PresignPhotoOutput struct {
	Body PresignPhotoResult
}

type ConfirmPhotoInput struct {
	CalendarID string `path:"calendarId"`
	PhotoID    string `path:"photoId"`
}

type ConfirmPhotoOutput struct {
	Body AlbumPhotoResponse
}

type UpdatePhotoBody struct {
	Caption patch.Field[string] `json:"caption,omitzero" required:"false" maxLength:"500" doc:"null clears the caption"`
	EventID patch.Field[string] `json:"eventId,omitzero" required:"false" doc:"null removes the event link"`
}

type UpdatePhotoInput struct {
	CalendarID string               `path:"calendarId"`
	PhotoID    string               `path:"photoId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
	Body       UpdatePhotoBody
}

type UpdatePhotoOutput struct {
	ETag string `header:"ETag"`
	Body AlbumPhotoResponse
}

type DeletePhotoInput struct {
	CalendarID string               `path:"calendarId"`
	PhotoID    string               `path:"photoId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
}

type DeletePhotoOutput struct{}

type DownloadPhotoInput struct {
	CalendarID string `path:"calendarId"`
	PhotoID    string `path:"photoId"`
}

type DownloadPhotoBody struct {
	DownloadURL string `json:"downloadUrl"`
}

type DownloadPhotoOutput struct {
	Body DownloadPhotoBody
}
