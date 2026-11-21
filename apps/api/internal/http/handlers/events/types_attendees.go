package events

// AttendeeResponse is one participant's state on an event: whether they have
// answered, and whether the owner has delegated editing to them.
type AttendeeResponse struct {
	UserID  string `json:"userId" doc:"public user ID"`
	Rsvp    string `json:"rsvp" enum:"pending,accepted,declined,tentative"`
	CanEdit bool   `json:"canEdit" doc:"read-only here; set through the attendee endpoint"`
}

type SetRsvpInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	Body       struct {
		Rsvp string `json:"rsvp" enum:"pending,accepted,declined,tentative"`
	}
}

type SetRsvpOutput struct {
	Body AttendeeResponse
}

type SetAttendeeCanEditInput struct {
	CalendarID string `path:"calendarId"`
	EventID    string `path:"eventId"`
	UserID     string `path:"userId"`
	Body       struct {
		CanEdit bool `json:"canEdit"`
	}
}

type SetAttendeeCanEditOutput struct {
	Body AttendeeResponse
}
