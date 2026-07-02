package calendars

import "time"

type CalendarResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CoverURL  string    `json:"coverUrl"`
	CreatedAt time.Time `json:"createdAt"`
	// PublicShared is true when an active public (read-only embed) link exists,
	// so the UI can flag the calendar as externally exposed.
	PublicShared bool `json:"publicShared"`
}

type MemberResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Icon  string `json:"icon"`
	Role  string `json:"role"`
	Color string `json:"color"`
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
		Name  string `json:"name" minLength:"1" maxLength:"200"`
		Color string `json:"color" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$"`
	}
}
type CreateCalendarOutput struct {
	Body CalendarResponse
}

type UpdateCalendarInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Name     string  `json:"name" minLength:"1" maxLength:"200"`
		Color    *string `json:"color,omitempty" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$" required:"false"`
		CoverURL *string `json:"coverUrl,omitempty" maxLength:"500" required:"false"`
	}
}
type UpdateCalendarOutput struct {
	Body CalendarResponse
}

type DeleteCalendarInput struct {
	CalendarID string `path:"calendarId"`
}
type DeleteCalendarOutput struct{}

// Members

type ListMembersInput struct {
	CalendarID string `path:"calendarId"`
}
type ListMembersOutput struct {
	Body []MemberResponse
}

type AddMemberInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Email string `json:"email" format:"email"`
		Role  string `json:"role" enum:"admin,member,viewer" default:"member"`
		Color string `json:"color" maxLength:"7" pattern:"^#[0-9A-Fa-f]{6}$"`
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
		Role string `json:"role" enum:"admin,member,viewer"`
	}
}
type UpdateMemberRoleOutput struct {
	Body MemberResponse
}

// Labels

type ListLabelsInput struct {
	CalendarID string `path:"calendarId"`
}
type ListLabelsOutput struct {
	Body []LabelResponse
}
