package calendars

import (
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
)

type CalendarResponse struct {
	ID string `json:"id"`
	// ETag names this revision of the calendar; send it as If-Match to change or delete it.
	ETag  string `json:"etag"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// CoverImageURL is a short-lived, signed URL for an uploaded cover image.
	CoverImageURL string `json:"coverImageUrl,omitempty"`
	// CoverUploadAllowed is true only for shared, user-created personal
	// calendars when signed blob storage is configured.
	CoverUploadAllowed bool      `json:"coverUploadAllowed"`
	CreatedAt          time.Time `json:"createdAt"`
	// PublicShared is true when an active public (read-only embed) link exists,
	// so the UI can flag the calendar as externally exposed.
	PublicShared bool `json:"publicShared"`
	// MemberCount is how many people the calendar is shared with, the
	// requesting user included, so the UI can tell a shared calendar from one
	// only its owner sees.
	MemberCount int `json:"memberCount"`
	// Role is the requesting user's own role on this calendar. It is what
	// decides which actions the client may offer, and it is reported here
	// because the calendar is never reached without resolving it -- a client
	// that had to work it out from the member list would be re-deriving an
	// answer the server already has.
	Role string `json:"role" enum:"owner,manager,editor,viewer"`
	// MemberColor is the colour the requesting user's own layer is drawn in on
	// this calendar. It belongs to the membership, not to the calendar.
	MemberColor string `json:"memberColor"`
}

type MemberResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Role      string `json:"role"`
	Color     string `json:"color"`
}

// LabelResponse is a named colour a calendar's events can be filed under.
type LabelResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color" doc:"upper-case #RRGGBB"`
	// ETag names this revision of the label; send it as If-Match to change or delete it.
	ETag string `json:"etag"`
}

// --- Inputs/Outputs ---

type ListCalendarsInput struct{}
type ListCalendarsOutput struct {
	Body []CalendarResponse
}

type GetCalendarInput struct {
	CalendarID string `path:"calendarId"`
}
type GetCalendarOutput struct {
	ETag string `header:"ETag"`
	Body CalendarResponse
}

type CreateCalendarInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"200"`
		// Omitted means "pick the default colour" -- the handler already does
		// that, and the pattern below rejects the empty string, so requiring
		// the field would make that fallback unreachable.
		Color string `json:"color,omitempty" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$" required:"false"`
	}
}
type CreateCalendarOutput struct {
	Body CalendarResponse
}

type UpdateCalendarInput struct {
	CalendarID string               `path:"calendarId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
	Body       struct {
		Name  patch.Field[string] `json:"name,omitzero" minLength:"1" maxLength:"200" required:"false"`
		Color patch.Field[string] `json:"color,omitzero" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$" required:"false"`
	}
}
type UpdateCalendarOutput struct {
	ETag string `header:"ETag"`
	Body CalendarResponse
}

type DeleteCalendarInput struct {
	CalendarID string               `path:"calendarId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
}
type DeleteCalendarOutput struct{}

type PresignCalendarCoverInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		SHA256      string `json:"sha256" pattern:"^[0-9a-f]{64}$" doc:"Lowercase hex SHA-256 of the image bytes"`
		ContentType string `json:"contentType" enum:"image/jpeg,image/png,image/webp"`
		ByteSize    int64  `json:"byteSize" minimum:"1" maximum:"5242880"`
	}
}
type PresignCalendarCoverResult struct {
	// CoverID is the opaque ID to send back to the confirm.
	CoverID string `json:"coverId"`
	// UploadURL is where to PUT the image. It is absent when the identical
	// bytes are already stored, in which case the confirm can follow at once.
	UploadURL string `json:"uploadUrl,omitempty"`
}
type PresignCalendarCoverOutput struct {
	Body PresignCalendarCoverResult
}

// PutCalendarCoverInput confirms an uploaded image as the calendar's cover.
type PutCalendarCoverInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		CoverID string `json:"coverId" minLength:"1" doc:"The ID the presign returned"`
	}
}
type PutCalendarCoverOutput struct {
	Body CalendarResponse
}

type DeleteCalendarCoverInput struct {
	CalendarID string `path:"calendarId"`
}
type DeleteCalendarCoverOutput struct{}

// Members

type ListMembersInput struct {
	CalendarID string `path:"calendarId"`
}
type ListMembersOutput struct {
	Body []MemberResponse
}

type ListMemberCandidatesInput struct {
	CalendarID string `path:"calendarId"`
	Query      string `query:"q" required:"false" maxLength:"100" doc:"Part of a name or email address"`
}

// MemberCandidateResponse is a workspace member who is not on the calendar
// yet and can be added to it directly.
type MemberCandidateResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}
type ListMemberCandidatesPage struct {
	Items []MemberCandidateResponse `json:"items"`
	// HasMore says more people match than were returned, so the picker asks
	// for a narrower query instead of implying the list is complete.
	HasMore bool `json:"hasMore"`
}
type ListMemberCandidatesOutput struct {
	Body ListMemberCandidatesPage
}

type AddMemberInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		UserID string `json:"userId" doc:"Public ID of a workspace member who is not on the calendar"`
		// Like a join link, a direct addition stops short of the roles that
		// hand out access; promotion goes through UpdateMemberRole.
		Role string `json:"role" enum:"editor,viewer"`
	}
}
type AddMemberOutput struct {
	Body MemberResponse
}

type RemoveMemberInput struct {
	CalendarID string `path:"calendarId"`
	UserID     string `path:"userId"`
}
type RemoveMemberOutput struct{}

type UpdateMemberRoleInput struct {
	CalendarID string `path:"calendarId"`
	UserID     string `path:"userId"`
	Body       struct {
		Role string `json:"role" enum:"owner,manager,editor,viewer"`
	}
}
type UpdateMemberRoleOutput struct {
	Body MemberResponse
}

type UpdateMemberColorInput struct {
	CalendarID string `path:"calendarId"`
	UserID     string `path:"userId"`
	Body       struct {
		Color string `json:"color" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$"`
	}
}
type UpdateMemberColorOutput struct {
	Body MemberResponse
}

// Labels

type ListLabelsInput struct {
	CalendarID string `path:"calendarId"`
}
type ListLabelsOutput struct {
	Body []LabelResponse
}

type CreateLabelInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Name  string `json:"name" minLength:"1" maxLength:"100" doc:"trimmed before it is stored; 1 to 50 characters after trimming"`
		Color string `json:"color" pattern:"^#[0-9A-Fa-f]{6}$" doc:"#RRGGBB; stored upper-case"`
	}
}
type CreateLabelOutput struct {
	ETag string `header:"ETag"`
	Body LabelResponse
}

// UpdateLabelInput is a merge patch; neither member can be cleared.
type UpdateLabelInput struct {
	CalendarID string               `path:"calendarId"`
	LabelID    string               `path:"labelId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
	Body       struct {
		Name  patch.Field[string] `json:"name,omitzero" minLength:"1" maxLength:"100" required:"false" doc:"trimmed before it is stored; 1 to 50 characters after trimming"`
		Color patch.Field[string] `json:"color,omitzero" pattern:"^#[0-9A-Fa-f]{6}$" required:"false" doc:"#RRGGBB; stored upper-case"`
	}
}
type UpdateLabelOutput struct {
	ETag string `header:"ETag"`
	Body LabelResponse
}

type DeleteLabelInput struct {
	CalendarID string               `path:"calendarId"`
	LabelID    string               `path:"labelId"`
	IfMatch    precondition.IfMatch `header:"If-Match" required:"true"`
}
type DeleteLabelOutput struct{}
