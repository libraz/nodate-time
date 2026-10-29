package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestDocumentedOptionalFieldsMayBeOmitted covers the bodies whose schema said a
// field was optional -- by carrying a default, by having a fallback in the
// handler, or by simply not being needed -- while the generated schema still
// listed it as required. The shipped SPA always sends every field, so the only
// caller that meets this is one written from the documentation.
func TestDocumentedOptionalFieldsMayBeOmitted(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)

	// A calendar created without a colour gets the default one. The pattern on
	// the field rejects the empty string, so requiring it left the handler's
	// own fallback unreachable.
	var cal struct {
		ID    string `json:"id"`
		Color string `json:"color"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/calendars", tenant.AccessToken,
		map[string]any{"name": "色なし"}, &cal)
	require.NotEmpty(t, cal.Color, "a calendar created without a colour should be given one")

	// An invite created without a role gets the documented default.
	var inv struct {
		Role string `json:"role"`
	}
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+tenant.CalendarID+"/invites", tenant.AccessToken,
		map[string]any{}, &inv)
	require.Equal(t, "editor", inv.Role, "the declared default should apply when role is omitted")

	// A memo needs a title and nothing else; its position is the caller's to
	// set later.
	var memo struct {
		ID        string `json:"id"`
		SortOrder int    `json:"sortOrder"`
	}
	helpers.DoJSON(t, http.MethodPost,
		testServerURL+"/calendars/"+tenant.CalendarID+"/memos", tenant.AccessToken,
		map[string]any{"title": "買い物"}, &memo)
	require.NotEmpty(t, memo.ID)
}

// TestMemoUpdateKeepsOmittedFields covers the update body, where an omitted
// field has to keep its stored value rather than arrive as a zero. Requiring
// every field made ticking a memo off a read-then-write, and a caller that got
// the read wrong would blank the body or move the memo to the top of the list.
func TestMemoUpdateKeepsOmittedFields(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	tenant := helpers.NewTenant(t, testServerURL)
	memosURL := testServerURL + "/calendars/" + tenant.CalendarID + "/memos"

	type memoBody struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Done      bool   `json:"done"`
		SortOrder int32  `json:"sortOrder"`
	}

	var created memoBody
	helpers.DoJSON(t, http.MethodPost, memosURL, tenant.AccessToken,
		map[string]any{"title": "打ち合わせ資料", "body": "第三会議室", "sortOrder": 7}, &created)
	require.NotEmpty(t, created.ID)

	// Ticking it off names only the flag. Everything else has to survive.
	var toggled memoBody
	helpers.DoJSON(t, http.MethodPut, memosURL+"/"+created.ID, tenant.AccessToken,
		map[string]any{"done": true}, &toggled)
	require.True(t, toggled.Done)
	require.Equal(t, "打ち合わせ資料", toggled.Title, "an omitted title should keep the stored one")
	require.Equal(t, "第三会議室", toggled.Body, "an omitted body should not be blanked")
	require.EqualValues(t, 7, toggled.SortOrder, "an omitted position should not move the memo")

	// Editing the text leaves the flag alone: an omitted done must not read as
	// false and reopen a memo that was finished.
	var edited memoBody
	helpers.DoJSON(t, http.MethodPut, memosURL+"/"+created.ID, tenant.AccessToken,
		map[string]any{"title": "打ち合わせ資料（改訂）"}, &edited)
	require.Equal(t, "打ち合わせ資料（改訂）", edited.Title)
	require.True(t, edited.Done, "an omitted done should keep the memo finished")
	require.Equal(t, "第三会議室", edited.Body)
	require.EqualValues(t, 7, edited.SortOrder)

	// A field that is present still applies, including one carrying a zero
	// value -- that is what separates omitted from explicitly set.
	var cleared memoBody
	helpers.DoJSON(t, http.MethodPut, memosURL+"/"+created.ID, tenant.AccessToken,
		map[string]any{"body": "", "done": false, "sortOrder": 0}, &cleared)
	require.Empty(t, cleared.Body, "an explicit empty body should clear it")
	require.False(t, cleared.Done, "an explicit false should reopen the memo")
	require.EqualValues(t, 0, cleared.SortOrder)
	require.Equal(t, "打ち合わせ資料（改訂）", cleared.Title)
}

// TestAllowedEmailNeedsNoReason covers the admin surface separately, since it
// takes a grant the ordinary sign-up flow does not hand out.
func TestAllowedEmailNeedsNoReason(t *testing.T) {
	bootstrap(t)
	// A live allow-list row restricts the whole instance: serial phase only.

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	// The allow-list is unique on the address and the row outlives a withdrawal, so the address has to differ per run.
	address := fmt.Sprintf("allowed-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() { withdrawAllowedEmail(t, address) })
	status, body := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": address})
	require.Equal(t, 200, status, "an allowed address should not require a stated reason: %s", string(body))
}
