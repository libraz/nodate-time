package calendars

import "time"

type CalendarResponse struct {
	ID    string `json:"id"`
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
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar,omitempty"`
	Role   string `json:"role"`
	Color  string `json:"color"`
}

type LabelResponse struct {
	ID      string `json:"id"`
	NameKey string `json:"nameKey" doc:"i18n key for label name (e.g. 'label.1')"`
	Color   string `json:"color"`
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
	CalendarID string `path:"calendarId"`
	Body       struct {
		Name  *string `json:"name,omitempty" minLength:"1" maxLength:"200" required:"false"`
		Color *string `json:"color,omitempty" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$" required:"false"`
	}
}
type UpdateCalendarOutput struct {
	Body CalendarResponse
}

type DeleteCalendarInput struct {
	CalendarID string `path:"calendarId"`
}
type DeleteCalendarOutput struct{}

type PutCalendarCoverInput struct {
	CalendarID string `path:"calendarId"`
	RawBody    []byte `contentType:"application/octet-stream"`
}
type PutCalendarCoverOutput struct {
	Body CalendarResponse
}

type DeleteCalendarCoverInput struct {
	CalendarID string `path:"calendarId"`
}
type DeleteCalendarCoverOutput struct {
	Body CalendarResponse
}

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
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Avatar string `json:"avatar,omitempty"`
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
