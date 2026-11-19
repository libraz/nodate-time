package schedulepolls

import "time"

type PollMemberResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PollSlotResponse struct {
	ID           string    `json:"id"`
	StartAt      time.Time `json:"startAt"`
	EndAt        time.Time `json:"endAt"`
	Availability string    `json:"availability" enum:"open,soft,hard"`
	Active       bool      `json:"active"`
}

type PollAnswerResponse struct {
	SlotID string `json:"slotId"`
	Value  string `json:"value" enum:"yes,maybe,no"`
}

type PollMemberAnswersResponse struct {
	MemberID string               `json:"memberId"`
	Answers  []PollAnswerResponse `json:"answers"`
}

type PollDetailResponse struct {
	ID               string                      `json:"id"`
	CalendarID       string                      `json:"calendarId"`
	Title            string                      `json:"title"`
	Timezone         string                      `json:"timezone"`
	Status           string                      `json:"status" enum:"open,finalized,cancelled"`
	Version          uint32                      `json:"version"`
	ResponseVersion  uint32                      `json:"responseVersion"`
	ReviewVersion    uint32                      `json:"reviewVersion"`
	CanManage        bool                        `json:"canManage"`
	CanTakeOver      bool                        `json:"canTakeOver,omitempty"`
	CanRespond       bool                        `json:"canRespond"`
	Slots            []PollSlotResponse          `json:"slots"`
	Members          []PollMemberResponse        `json:"members"`
	MyAnswers        []PollAnswerResponse        `json:"myAnswers"`
	Responses        []PollMemberAnswersResponse `json:"responses,omitempty"`
	RespondedCount   int64                       `json:"respondedCount"`
	MemberCount      int64                       `json:"memberCount"`
	ConfirmedEventID string                      `json:"confirmedEventId,omitempty"`
	SelectedSlotID   string                      `json:"selectedSlotId,omitempty"`
}

type PollSlotInput struct {
	ID      string `json:"id,omitempty" required:"false"`
	StartAt string `json:"startAt"`
	EndAt   string `json:"endAt"`
}

type PollAnswerInput struct {
	SlotID string `json:"slotId"`
	Value  string `json:"value" enum:"yes,maybe,no"`
}

type CandidateSlotResponse struct {
	StartAt      time.Time `json:"startAt"`
	EndAt        time.Time `json:"endAt"`
	Availability string    `json:"availability" enum:"open,soft,hard"`
	Active       bool      `json:"active"`
}

type AvailabilityMemberInput struct {
	MemberIDs []string `json:"memberIds"`
}

type CalendarMembersInput struct {
	CalendarID string `path:"calendarId"`
}

type CalendarMembersOutput struct {
	Body struct {
		Members []PollMemberResponse `json:"members"`
	}
}

type AvailabilitySharingInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Enabled bool `json:"enabled"`
	}
}

type AvailabilitySharingOutput struct {
	Body struct {
		Enabled bool `json:"enabled"`
	}
}

type SearchAvailabilityInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Timezone        string          `json:"timezone" maxLength:"64"`
		FromDate        string          `json:"fromDate,omitempty" required:"false"`
		ToDate          string          `json:"toDate,omitempty" required:"false"`
		StartTime       string          `json:"startTime,omitempty" required:"false"`
		EndTime         string          `json:"endTime,omitempty" required:"false"`
		DurationMinutes int             `json:"durationMinutes,omitempty" required:"false"`
		MemberIDs       []string        `json:"memberIds"`
		Slots           []PollSlotInput `json:"slots,omitempty" required:"false"`
	}
}

type SearchAvailabilityOutput struct {
	Body struct {
		Slots []CandidateSlotResponse `json:"slots"`
	}
}

type CreatePollInput struct {
	CalendarID string `path:"calendarId"`
	Body       struct {
		Title     string          `json:"title" minLength:"1" maxLength:"500"`
		Timezone  string          `json:"timezone" maxLength:"64"`
		MemberIDs []string        `json:"memberIds"`
		Slots     []PollSlotInput `json:"slots"`
	}
}

type CreatePollOutput struct{ Body PollDetailResponse }

type ListPollsInput struct {
	CalendarID string `path:"calendarId"`
}
type ListPollsOutput struct {
	Body struct {
		Polls []PollDetailResponse `json:"polls"`
	}
}

type GetPollInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
}
type GetPollOutput struct{ Body PollDetailResponse }

type UpdatePollInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
	Body       struct {
		Version   uint32          `json:"version"`
		Title     *string         `json:"title,omitempty" required:"false" minLength:"1" maxLength:"500"`
		MemberIDs []string        `json:"memberIds,omitempty" required:"false"`
		Slots     []PollSlotInput `json:"slots,omitempty" required:"false"`
		TakeOver  bool            `json:"takeOver,omitempty" required:"false"`
	}
}
type UpdatePollOutput struct{ Body PollDetailResponse }

type PollAction struct {
	CalendarID   string `json:"calendarId"`
	CalendarName string `json:"calendarName"`
	PollID       string `json:"pollId"`
	Title        string `json:"title"`
	Kind         string `json:"kind" enum:"answer,finalize,add-slots"`
}

type PollActionsOutput struct {
	Body struct {
		Actions []PollAction `json:"actions"`
	}
}

type PollMutationInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
}

type PollMutationOutput struct{ Body PollDetailResponse }

type SetResponseInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
	Body       struct {
		Version         uint32            `json:"version"`
		ResponseVersion uint32            `json:"responseVersion"`
		Answers         []PollAnswerInput `json:"answers"`
	}
}

type FinalizePollInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
	Body       struct {
		Version       uint32  `json:"version"`
		ReviewVersion uint32  `json:"reviewVersion"`
		SlotID        string  `json:"slotId"`
		OwnerID       *string `json:"ownerId,omitempty" required:"false"`
		Acknowledged  bool    `json:"acknowledged,omitempty" required:"false"`
	}
}

type CancelPollInput struct {
	CalendarID string `path:"calendarId"`
	PollID     string `path:"pollId"`
	Body       struct {
		Version uint32 `json:"version"`
	}
}

type PollActionsInput struct{}
