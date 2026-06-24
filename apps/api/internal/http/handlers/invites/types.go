package invites

import "time"

type InviteResponse struct {
	ID        uint32     `json:"id"`
	Token     string     `json:"token"`
	Role      string     `json:"role"`
	MaxUses   *uint32    `json:"maxUses,omitempty"`
	UseCount  uint32     `json:"useCount"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type CreateInviteInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Role           string `json:"role" enum:"member,viewer" default:"member"`
		MaxUses        *int32 `json:"maxUses,omitempty" required:"false" minimum:"1"`
		ExpiresInHours *int   `json:"expiresInHours,omitempty" required:"false" minimum:"1" maximum:"8760"`
	}
}
type CreateInviteOutput struct {
	Body InviteResponse
}

type AcceptInviteInput struct {
	Token string `path:"token"`
}
type AcceptInviteOutput struct {
	Body struct {
		CalendarID string `json:"calendarId"`
		Role       string `json:"role"`
	}
}

// --- Invite management ---

type ListInvitesInput struct {
	CalendarID string `path:"calendarId"`
}
type ListInvitesOutput struct {
	Body []InviteResponse
}

type DeleteInviteInput struct {
	CalendarID string `path:"calendarId"`
	InviteID   uint32 `path:"inviteId"`
}
type DeleteInviteOutput struct{}

// --- Public share ---

type PublicCalendarInput struct {
	Token string `path:"token"`
}
type PublicCalendarOutput struct {
	Body struct {
		CalendarID string `json:"calendarId"`
		Name       string `json:"name"`
		Color      string `json:"color"`
	}
}

type PublicEventsInput struct {
	Token     string `path:"token"`
	StartDate string `query:"start" required:"false"`
	EndDate   string `query:"end" required:"false"`
	Days      int    `query:"days" minimum:"1" default:"30" required:"false"`
}
type PublicEventsOutput struct {
	Body []PublicEventResponse
}

type PublicEventResponse struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	AllDay   bool      `json:"allDay"`
	StartAt  time.Time `json:"startAt"`
	EndAt    time.Time `json:"endAt"`
	Color    string    `json:"color"`
	Location string    `json:"location,omitempty"`
}
