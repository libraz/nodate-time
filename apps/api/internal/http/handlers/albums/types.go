package albums

import "time"

type AlbumUploader struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

type AlbumPhotoResponse struct {
	ID          string        `json:"id"`
	CalendarID  string        `json:"calendarId"`
	Caption     string        `json:"caption"`
	ContentType string        `json:"contentType"`
	ByteSize    int64         `json:"byteSize"`
	Width       *int          `json:"width,omitempty"`
	Height      *int          `json:"height,omitempty"`
	EventID     string        `json:"eventId,omitempty"`
	TakenAt     time.Time     `json:"takenAt"`
	CreatedAt   time.Time     `json:"createdAt"`
	UploadedBy  AlbumUploader `json:"uploadedBy"`
	ImageURL    string        `json:"imageUrl"`
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
	Caption     string    `json:"caption" required:"false" maxLength:"500"`
	EventID     string    `json:"eventId" required:"false" doc:"Optional event public ID"`
	TakenAt     time.Time `json:"takenAt" required:"false" doc:"EXIF capture time. Defaults to upload time."`
	Width       int       `json:"width" required:"false" minimum:"1"`
	Height      int       `json:"height" required:"false" minimum:"1"`
}

type PresignPhotoInput struct {
	CalendarID string `path:"calendarId"`
	Body       PresignPhotoBody
}

type PresignPhotoResult struct {
	PhotoID    string `json:"photoId"`
	UploadURL  string `json:"uploadUrl"`
	StorageKey string `json:"storageKey"`
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
	Caption *string `json:"caption" required:"false" maxLength:"500"`
	EventID *string `json:"eventId" required:"false" doc:"Empty string clears the link"`
}

type UpdatePhotoInput struct {
	CalendarID string `path:"calendarId"`
	PhotoID    string `path:"photoId"`
	Body       UpdatePhotoBody
}

type UpdatePhotoOutput struct {
	Body AlbumPhotoResponse
}

type DeletePhotoInput struct {
	CalendarID string `path:"calendarId"`
	PhotoID    string `path:"photoId"`
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
