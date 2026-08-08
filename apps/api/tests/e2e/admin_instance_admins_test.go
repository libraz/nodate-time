package e2e

import (
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

type instanceAdminRow struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	GrantedAt   time.Time `json:"grantedAt"`
}

type instanceAdminList struct {
	Admins []instanceAdminRow `json:"admins"`
}

func listInstanceAdmins(t *testing.T, token string) instanceAdminList {
	t.Helper()
	var list instanceAdminList
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/instance-admins", token, nil, &list)
	return list
}

// TestInstanceAdminListingNamesWhoHoldsTheRights covers the read half of an
// operation that had no endpoint at all: finding out who can administer the
// instance meant running a SELECT against the database by hand.
//
// The listing is instance-wide and other tests grant their own administrators,
// so this asserts the caller's own grant is present rather than the size of the
// list.
func TestInstanceAdminListingNamesWhoHoldsTheRights(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	list := listInstanceAdmins(t, admin.AccessToken)

	var found *instanceAdminRow
	for i := range list.Admins {
		if list.Admins[i].UserID == admin.UserID {
			found = &list.Admins[i]
		}
	}
	require.NotNil(t, found, "the caller holds the rights and should appear in the list of who does")
	require.Equal(t, admin.Email, found.Email, "an administrator is identified by address, not by id alone")
	require.Equal(t, admin.Name, found.DisplayName)
	require.False(t, found.GrantedAt.IsZero(), "the grant records when it was made and the listing should say so")
}

// TestInstanceAdminListingExposesNoInternalIDs pins the two-tier id rule on a
// listing that is generated from a row carrying three internal ids: the grant's
// own, the user's, and the granter's. The internal ids are one AUTO_INCREMENT
// sequence for the whole deployment, so handing them out tells a reader how
// many accounts and grants exist.
func TestInstanceAdminListingExposesNoInternalIDs(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	var raw struct {
		Admins []map[string]any `json:"admins"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/admin/instance-admins",
		admin.AccessToken, nil, &raw)
	require.NotEmpty(t, raw.Admins)

	for _, entry := range raw.Admins {
		keys := make([]string, 0, len(entry))
		for k := range entry {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		require.Equal(t, []string{"displayName", "email", "grantedAt", "userId"}, keys,
			"the listing serves exactly these fields; an internal id must not arrive by accident")

		id, _ := entry["userId"].(string)
		_, err := uuid.Parse(id)
		require.NoError(t, err, "an administrator must be named by a public id, got %q", id)
	}
}

// TestInstanceAdminListingIsAdminOnly checks the group the route was registered
// in. Who administers the instance is not a question an ordinary account gets
// to ask, and registering the route in the wrong group is a one-line mistake
// that nothing else would catch.
func TestInstanceAdminListingIsAdminOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	outsider := helpers.NewTenant(t, testServerURL)

	status, body := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/admin/instance-admins", outsider.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status, "body: %s", string(body))
}

// TestRevokingAnotherAdminTakesTheirRightsAway is the operation the endpoint
// exists for: until it was wired, removing an administrator meant a DELETE
// typed against the database, which is a poor thing to need at the moment
// somebody's access has to go.
func TestRevokingAnotherAdminTakesTheirRightsAway(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	keeper := helpers.NewTenant(t, testServerURL)
	leaver := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, keeper.UserID)
	helpers.MakeInstanceAdmin(t, testDB, leaver.UserID)

	// The rights are real before they are taken away, or the assertion after
	// the revocation would pass against an account that never had them.
	status, body := helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/admin/instance-admins", leaver.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "body: %s", string(body))

	// The handler has no path that revokes without opening the transaction, so
	// this call runs the locking count as well as the write -- the guard is
	// executed here even though nothing it protects against is happening.
	status, body = helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+leaver.UserID, keeper.AccessToken, nil)
	require.Equal(t, http.StatusNoContent, status, "body: %s", string(body))

	status, body = helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/admin/instance-admins", leaver.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status,
		"a revoked administrator must lose the admin surface, body: %s", string(body))

	// And they are gone from the listing, not merely refused at the door.
	for _, entry := range listInstanceAdmins(t, keeper.AccessToken).Admins {
		require.NotEqual(t, leaver.UserID, entry.UserID,
			"a revoked grant must leave the listing of who holds the rights")
	}
}

// The count guard in RevokeInstanceAdmin -- the one refusing a revocation that
// would leave nobody -- has no test that makes it refuse, and cannot have one
// here.
//
// instance_admins is deliberately not workspace-scoped, so its count is one
// number for the whole database. This suite runs in parallel and every tenant
// that needs the admin surface grants itself a row, so the count during a run
// is never 1 and the guard's refusing branch is unreachable. A test asserting
// it would assert nothing while reading as coverage.
//
// What would have to change: the count would need a scope a test could own --
// per-workspace grants, or a fixture database per package -- or the whole run
// would have to serialise, which trades a real property of this suite for one
// branch. Until then the branch below exercises the guard on its passing path,
// and self-revocation carries the rule.

// TestAnAdminCannotRevokeThemselves pins the rule that keeps an instance
// administrable. Revoking the last administrator leaves nobody who can grant
// the rights back -- there is no API that hands them out -- so the recovery is
// a hand-written statement against the database.
//
// Refusing self-revocation is the half of that guard which can be stated as a
// rule about one request. It is also the shape the accident takes: an
// administrator clicking remove on their own row.
func TestAnAdminCannotRevokeThemselves(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)

	status, body := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+admin.UserID, admin.AccessToken, nil)
	require.Equal(t, http.StatusBadRequest, status, "body: %s", string(body))
	require.Contains(t, string(body), "ADMIN.SELF_REVOKE")

	// The refusal has to be a refusal, not a message in front of a write.
	status, _ = helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/admin/instance-admins", admin.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "the caller must still be an administrator")
}

// TestRevokingSomebodyWhoIsNotAnAdminReportsAMiss covers the case an operator
// most needs told: they believe they removed somebody's access and did not.
// A silent success there leaves live rights that everyone thinks are gone.
func TestRevokingSomebodyWhoIsNotAnAdminReportsAMiss(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	admin := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, admin.UserID)
	ordinary := helpers.NewTenant(t, testServerURL)

	status, body := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+ordinary.UserID, admin.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "body: %s", string(body))

	// An id that names nobody is the same answer, and must not be a 500.
	status, body = helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+uuid.NewString(), admin.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "body: %s", string(body))

	// So is a string that is not an id at all.
	status, body = helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/not-a-uuid", admin.AccessToken, nil)
	require.Equal(t, http.StatusNotFound, status, "body: %s", string(body))
}

// TestRevokingAnAdminIsAdminOnly guards the route group, as for the listing:
// taking away administrator rights is the single most damaging thing this
// surface can do.
func TestRevokingAnAdminIsAdminOnly(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	target := helpers.NewTenant(t, testServerURL)
	helpers.MakeInstanceAdmin(t, testDB, target.UserID)
	outsider := helpers.NewTenant(t, testServerURL)

	status, body := helpers.DoJSONStatus(t, http.MethodDelete,
		testServerURL+"/admin/instance-admins/"+target.UserID, outsider.AccessToken, nil)
	require.Equal(t, http.StatusForbidden, status, "body: %s", string(body))

	// The refused call changed nothing.
	status, _ = helpers.DoJSONStatus(t, http.MethodGet,
		testServerURL+"/admin/instance-admins", target.AccessToken, nil)
	require.Equal(t, http.StatusOK, status, "the target must still be an administrator")
}
