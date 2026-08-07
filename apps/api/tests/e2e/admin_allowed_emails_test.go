package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// TestAllowedEmailReasonSurvivesTheRoundTrip pins the field name the allow-list
// is written and read under. An administrator states why an address is
// excepted; if the value is accepted under one name and served under another it
// is stored and never seen again, and the list is unreadable a year later.
func TestAllowedEmailReasonSurvivesTheRoundTrip(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	// The allow-list is unique on the address and this test leaves its row
	// behind, so the address has to differ per run.
	address := fmt.Sprintf("reasoned-%d@example.com", time.Now().UnixNano())
	reason := fmt.Sprintf("contractor until %d", time.Now().UnixNano())

	var created struct {
		ID     string `json:"id"`
		Email  string `json:"email"`
		Reason string `json:"reason"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		map[string]any{"email": address, "reason": reason}, &created)
	require.Equal(t, reason, created.Reason, "the creation response should echo the stated reason")

	var list struct {
		Emails []struct {
			ID     string `json:"id"`
			Email  string `json:"email"`
			Reason string `json:"reason"`
		} `json:"emails"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/allowed-emails", admin.AccessToken,
		nil, &list)

	var found bool
	for _, e := range list.Emails {
		if e.Email == address {
			found = true
			require.Equal(t, reason, e.Reason, "the listing should serve the reason that was written")
		}
	}
	require.True(t, found, "the address that was just allowed should be in the list")
}
