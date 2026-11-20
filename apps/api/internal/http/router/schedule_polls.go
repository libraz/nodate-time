package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/schedulepolls"
)

func registerSchedulePolls(api huma.API, deps Deps) {
	polls := schedulepolls.Deps{DB: deps.DB, Queries: deps.Queries, WorkspaceID: deps.WorkspaceID, Storage: deps.Storage}
	const base = "/calendars/{calendarId}/schedule-polls"
	const detail = base + "/{pollId}"
	operation := func(id, method, path, summary string) huma.Operation {
		return huma.Operation{
			OperationID: id,
			Method:      method,
			Path:        path,
			Summary:     summary,
			Tags:        []string{"Schedule poll"},
		}
	}

	huma.Register(api, operation("get-schedule-availability-sharing", http.MethodGet, base+"/availability-sharing", "Get calendar availability sharing"), schedulepolls.GetAvailabilitySharing(polls))
	huma.Register(api, operation("set-schedule-availability-sharing", http.MethodPut, base+"/availability-sharing", "Set calendar availability sharing"), schedulepolls.SetAvailabilitySharing(polls))
	huma.Register(api, operation("search-schedule-availability", http.MethodPost, base+"/availability", "Find available candidate times"), schedulepolls.SearchAvailability(polls))

	create := operation("create-schedule-poll", http.MethodPost, base, "Send a schedule poll to selected calendar members")
	create.DefaultStatus = http.StatusCreated
	huma.Register(api, create, schedulepolls.CreatePoll(polls))
	huma.Register(api, operation("list-schedule-polls", http.MethodGet, base, "List the caller's schedule polls in a calendar"), schedulepolls.ListPolls(polls))
	huma.Register(api, operation("get-schedule-poll", http.MethodGet, detail, "Get a schedule poll"), schedulepolls.GetPoll(polls))
	huma.Register(api, operation("update-schedule-poll", http.MethodPatch, detail, "Change the recipients or candidate times"), schedulepolls.UpdatePoll(polls))
	huma.Register(api, operation("refresh-schedule-poll-availability", http.MethodPost, detail+"/refresh-availability", "Recheck the remaining candidate times"), schedulepolls.RefreshAvailability(polls))
	huma.Register(api, operation("set-schedule-poll-response", http.MethodPut, detail+"/response", "Save the caller's response"), schedulepolls.SetResponse(polls))
	huma.Register(api, operation("finalize-schedule-poll", http.MethodPost, detail+"/finalize", "Create a calendar event from the selected candidate"), schedulepolls.FinalizePoll(polls))
	huma.Register(api, operation("cancel-schedule-poll", http.MethodPost, detail+"/cancel", "Cancel an open schedule poll"), schedulepolls.CancelPoll(polls))
	huma.Register(api, operation("list-my-schedule-poll-actions", http.MethodGet, "/me/schedule-poll-actions", "List the caller's pending schedule poll actions"), schedulepolls.SchedulePollActions(polls))
}
