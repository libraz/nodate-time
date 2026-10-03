package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// Withdrawing an address and allowing it again re-enables the stored row. The
// id the second creation answers with has to be that row's, or the listing
// shows one id and deleting by the other reports it missing.
func TestAllowedEmailCanBeWithdrawnReallowedAndDeleted(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)
	address := fmt.Sprintf("cycle-%d@example.com", time.Now().UnixNano())

	type allowed struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	create := func() allowed {
		var out allowed
		helpers.DoJSON(t, http.MethodPost, testServerURL+"/admin/allowed-emails", admin.AccessToken,
			map[string]any{"email": address, "reason": "cycle"}, &out)
		return out
	}
	listedID := func() string {
		var list struct {
			Emails []allowed `json:"emails"`
		}
		helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/allowed-emails", admin.AccessToken, nil, &list)
		for _, e := range list.Emails {
			if e.Email == address {
				return e.ID
			}
		}
		return ""
	}
	remove := func(id string) int {
		status, _ := helpers.DoJSONStatus(t, http.MethodDelete, testServerURL+"/admin/allowed-emails/"+id, admin.AccessToken, nil)
		return status
	}

	first := create()
	require.Equal(t, first.ID, listedID())
	require.Equal(t, http.StatusNoContent, remove(first.ID))
	require.Empty(t, listedID(), "a withdrawn address is no longer listed")

	again := create()
	require.Equal(t, listedID(), again.ID, "the re-allow response names the id the listing shows")
	require.Equal(t, http.StatusNoContent, remove(again.ID), "the id the re-allow answered with deletes it")
	require.Empty(t, listedID())
}
