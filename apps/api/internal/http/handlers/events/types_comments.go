package events

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

type CommentResponse struct {
	ID              string    `json:"id"`
	AuthorID        string    `json:"authorId"`
	AuthorName      string    `json:"authorName"`
	AuthorAvatarURL string    `json:"authorAvatarUrl,omitempty"`
	Body            string    `json:"body"`
	ETag            string    `json:"etag" doc:"Version of this comment; send it as If-Match on a change"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Comments

type ListCommentsInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Cursor     string `query:"cursor" required:"false" doc:"Opaque cursor from a previous response; pages backwards into older comments"`
	Limit      int    `query:"limit" required:"false" minimum:"1" maximum:"200" default:"50"`
}

// ListCommentsPage carries one page in reading order, oldest first. The page
// itself is taken from the newest end of the thread, and nextCursor asks for
// the one before it.
type ListCommentsPage struct {
	Items      []CommentResponse `json:"items"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type ListCommentsOutput struct {
	Body ListCommentsPage
}

type CreateCommentInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Body string `json:"body" minLength:"1" maxLength:"16000"`
	}
}
type CreateCommentOutput struct {
	ETag string `header:"ETag" required:"false"`
	Body CommentResponse
}

// UpdateCommentInput is a merge patch; body does not accept null.
type UpdateCommentInput struct {
	CalendarID string               `path:"calendarId"`
	EventID    string               `path:"eventId"`
	CommentID  string               `path:"commentId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true" doc:"etag of the comment being changed"`
	Body       struct {
		Body patch.Field[string] `json:"body,omitzero" required:"false" minLength:"1" maxLength:"16000" doc:"omit to keep the current text"`
	}
}
type UpdateCommentOutput struct {
	ETag string `header:"ETag" required:"false"`
	Body CommentResponse
}

type DeleteCommentInput struct {
	CalendarID string               `path:"calendarId"`
	EventID    string               `path:"eventId"`
	CommentID  string               `path:"commentId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true" doc:"etag of the comment being deleted"`
}
type DeleteCommentOutput struct{}
