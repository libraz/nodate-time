package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/apppasswords"
)

// registerAppPasswords registers the app password management routes on the
// protected group.
func registerAppPasswords(g apiGroups, deps Deps) {
	api := g.protected
	appDeps := apppasswords.Deps{Queries: deps.Queries, WorkspaceID: deps.WorkspaceID, APIPublicURL: deps.APIPublicURL}

	huma.Register(api, huma.Operation{
		OperationID: "list-app-passwords",
		Method:      http.MethodGet,
		Path:        "/user/app-passwords",
		Summary:     "List the caller's app passwords",
		Tags:        []string{"User"},
	}, apppasswords.ListAppPasswords(appDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "create-app-password",
		Method:        http.MethodPost,
		Path:          "/user/app-passwords",
		Summary:       "Create an app password for a calendar app",
		Tags:          []string{"User"},
		DefaultStatus: http.StatusCreated,
		Errors: errorStatuses(apierrors.AppPasswordEmailUnverified, apierrors.AppPasswordMFAEnabled,
			apierrors.AppPasswordLimitReached),
	}, apppasswords.CreateAppPassword(appDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-app-password",
		Method:        http.MethodDelete,
		Path:          "/user/app-passwords/{appPasswordId}",
		Summary:       "Revoke one of the caller's app passwords",
		Tags:          []string{"User"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errorStatuses(apierrors.AppPasswordNotFound),
	}, apppasswords.RevokeAppPassword(appDeps))
}
