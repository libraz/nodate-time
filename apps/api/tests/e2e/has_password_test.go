package e2e

import (
	"context"
	"net/http"
	"testing"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

func hasPasswordOf(t *testing.T, tt *helpers.TestTenant) bool {
	t.Helper()
	var me struct {
		HasPassword bool `json:"hasPassword"`
	}
	helpers.DoJSON(t, http.MethodGet, testServerURL+"/user", tt.AccessToken, nil, &me)
	return me.HasPassword
}

// An account reports whether it holds a local password, so the client can
// leave out a password change that cannot apply to a provider-only account.
func TestUserReportsWhetherItHasAPassword(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	ctx := context.Background()
	q := generated.New(testDB)

	t.Run("password account", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		require.True(t, hasPasswordOf(t, tt))
	})

	t.Run("local identity without a hash", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		user, err := q.GetUserByEmail(ctx, tt.Email)
		require.NoError(t, err)
		require.NoError(t, q.UpdatePasswordHash(ctx, generated.UpdatePasswordHashParams{UserID: user.ID}))
		require.False(t, hasPasswordOf(t, tt))
	})

	t.Run("no local identity", func(t *testing.T) {
		tt := helpers.NewTenant(t, testServerURL)
		user, err := q.GetUserByEmail(ctx, tt.Email)
		require.NoError(t, err)
		require.NoError(t, q.DisableIdentity(ctx, generated.DisableIdentityParams{UserID: user.ID, Provider: "local"}))
		require.False(t, hasPasswordOf(t, tt))
	})
}
