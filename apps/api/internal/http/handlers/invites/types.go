package invites

import "time"

type InviteResponse struct {
	ID string `json:"id"`
	// Token is the plaintext share link, returned only by the request that
	// created it. Only its hash is stored, so listing invites cannot hand
	// the link back -- a database read must not yield a working capability.
	Token     string     `json:"token,omitempty"`
	Role      string     `json:"role"`
	MaxUses   *uint32    `json:"maxUses,omitempty"`
	UseCount  uint32     `json:"useCount"`
	IsPublic  bool       `json:"isPublic"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type CreateInviteInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		// An invite link may grant editing or reading, never administration:
		// a link that could hand out management would let whoever holds it
		// grant themselves the power to hand out more.
		// A default only takes effect on a field the schema lets the caller
		// omit, so role carries required:"false" alongside it.
		Role           string `json:"role,omitempty" enum:"editor,viewer" default:"editor" required:"false"`
		MaxUses        *int32 `json:"maxUses,omitempty" required:"false" minimum:"1"`
		ExpiresInHours *int   `json:"expiresInHours,omitempty" required:"false" minimum:"1" maximum:"8760"`
		// IsPublic marks a read-only embed link that cannot be joined.
		IsPublic *bool `json:"isPublic,omitempty" required:"false"`
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
	InviteID   string `path:"inviteId"`
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
		// Joinable is false for public, read-only embed links; the frontend hides
		// the join action so such links cannot grant membership. It is also
		// false once a join link has been used up.
		Joinable bool `json:"joinable"`
		// Spent distinguishes the two reasons a link cannot be joined, so the
		// page can say the link is finished rather than silently drop the
		// button and leave the visitor looking for it.
		Spent bool `json:"spent"`
	}
}

type PublicEventsInput struct {
	Token     string `path:"token"`
	StartDate string `query:"start" required:"false"`
	EndDate   string `query:"end" required:"false"`
	Days      int    `query:"days" minimum:"1" maximum:"366" default:"30" required:"false"`
	// TZ names the zone the requested dates are days in. A public link has no
	// account behind it to take the answer from, so the page asking says which
	// zone it is rendering.
	TZ string `query:"tz" required:"false" doc:"IANA timezone the dates are read in"`
}
type PublicEventsOutput struct {
	// Truncated says the listing stopped at the per-request instance cap.
	Truncated string `header:"X-Result-Truncated" required:"false"`
	Body      []PublicEventResponse
}

type PublicEventResponse struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	AllDay   bool      `json:"allDay"`
	StartAt  time.Time `json:"startAt"`
	EndAt    time.Time `json:"endAt"`
	Timezone string    `json:"timezone"`
	Color    string    `json:"color"`
	Location string    `json:"location,omitempty"`
	// Private is true when the event's visibility keeps its details from a
	// public viewer. Title and location are withheld in that case, so the
	// page shows that the time is taken without saying by what.
	Private bool `json:"private"`
}
