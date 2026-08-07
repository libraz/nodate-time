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

// TestAllowedEmailNeedsNoReason covers the admin surface separately, since it
// takes a grant the ordinary sign-up flow does not hand out.
func TestAllowedEmailNeedsNoReason(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	// The allow-list is unique on the address and this test leaves its row
	// behind, so the address has to differ per run.
	address := fmt.Sprintf("allowed-%d@example.com", time.Now().UnixNano())
	status, body := helpers.DoJSONStatus(t, http.MethodPost,
		testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": address})
	require.Equal(t, 200, status, "an allowed address should not require a stated reason: %s", string(body))
}
