package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/admin"
)

func registerAdmin(g apiGroups, deps Deps) {
	api := g.admin

	envHas := func(p string) bool {
		switch p {
		case "google":
			return deps.OAuth.Google.ClientID != ""
		case "line":
			return deps.OAuth.LINE.ClientID != ""
		}
		return false
	}
	adminDeps := admin.Deps{DB: deps.DB, Queries: deps.Queries, Cipher: deps.Cipher, EnvFallback: envHas, AllowedDomains: deps.GoogleAllowedDomains}

	huma.Register(api, huma.Operation{
		OperationID: "list-oauth-providers",
		Method:      http.MethodGet,
		Path:        "/admin/oauth-providers",
		Summary:     "List configured OAuth providers (admin only)",
		Tags:        []string{"Admin"},
	}, admin.ListOAuthProviders(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-oauth-provider",
		Method:      http.MethodPatch,
		Path:        "/admin/oauth-providers/{provider}",
		Summary:     "Update OAuth provider credentials (admin only)",
		Tags:        []string{"Admin"},
		Errors:      errorStatuses(apierrors.BadRequest, apierrors.Validation, apierrors.SecretsUnavailable),
	}, admin.UpdateOAuthProvider(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-oauth-provider",
		Method:        http.MethodDelete,
		Path:          "/admin/oauth-providers/{provider}",
		Summary:       "Delete OAuth provider configuration (admin only)",
		Tags:          []string{"Admin"},
		DefaultStatus: 204,
	}, admin.DeleteOAuthProvider(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID: "list-instance-admins",
		Method:      http.MethodGet,
		Path:        "/admin/instance-admins",
		Summary:     "List instance administrators (admin only)",
		Tags:        []string{"Admin"},
	}, admin.ListInstanceAdmins(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "add-instance-admin",
		Method:        http.MethodPost,
		Path:          "/admin/instance-admins",
		Summary:       "Grant a user instance administrator rights (admin only)",
		Tags:          []string{"Admin"},
		DefaultStatus: http.StatusCreated,
		Errors: errorStatuses(apierrors.AdminUserNotFound, apierrors.AdminAlreadyAdmin,
			apierrors.AdminAdminsLimitReached),
	}, admin.AddInstanceAdmin(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-instance-admin",
		Method:        http.MethodDelete,
		Path:          "/admin/instance-admins/{userId}",
		Summary:       "Revoke a user's instance administrator rights (admin only)",
		Tags:          []string{"Admin"},
		DefaultStatus: http.StatusNoContent,
	}, admin.RevokeInstanceAdmin(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID: "list-allowed-emails",
		Method:      http.MethodGet,
		Path:        "/admin/allowed-emails",
		Summary:     "List individually allowed OAuth sign-in emails (admin only)",
		Tags:        []string{"Admin"},
	}, admin.ListAllowedEmails(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-allowed-email",
		Method:        http.MethodPost,
		Path:          "/admin/allowed-emails",
		Summary:       "Allow an individual email to sign in via OAuth (admin only)",
		Tags:          []string{"Admin"},
		DefaultStatus: http.StatusCreated,
		Errors: errorStatuses(apierrors.AuthEmailUnsupported, apierrors.Conflict,
			apierrors.AdminAllowedEmailsLimitReached),
	}, admin.CreateAllowedEmail(adminDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-allowed-email",
		Method:        http.MethodDelete,
		Path:          "/admin/allowed-emails/{emailId}",
		Summary:       "Remove an individually allowed email (admin only)",
		Tags:          []string{"Admin"},
		DefaultStatus: 204,
	}, admin.DeleteAllowedEmail(adminDeps))
}
