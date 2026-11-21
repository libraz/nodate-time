package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

func registerUsers(g apiGroups, deps Deps) {
	api := g.protected
	mailOn := mailer.Configured(deps.Mailer)
	userDeps := newUserDeps(deps)

	// User
	huma.Register(api, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/user",
		Summary:     "Get current user",
		Tags:        []string{"User"},
	}, users.GetMe(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-me",
		Method:      http.MethodPut,
		Path:        "/user",
		Summary:     "Update current user",
		Tags:        []string{"User"},
	}, users.UpdateMe(userDeps))

	if mailOn {
		huma.Register(api, huma.Operation{
			OperationID: "resend-email-verification",
			Method:      http.MethodPost,
			Path:        "/user/verify-email/resend",
			Summary:     "Send a fresh address-confirmation email",
			Tags:        []string{"User"},
		}, users.ResendEmailVerification(userDeps))
	}

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      http.MethodPost,
		Path:        "/auth/logout",
		Summary:     "Revoke the session this request authenticated with",
		Tags:        []string{"Auth"},
	}, users.Logout(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "list-sessions",
		Method:      http.MethodGet,
		Path:        "/user/sessions",
		Summary:     "List the caller's live sign-ins",
		Tags:        []string{"User"},
	}, users.ListSessions(userDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "revoke-session",
		Method:        http.MethodDelete,
		Path:          "/user/sessions/{sessionId}",
		Summary:       "End one of the caller's sign-ins",
		Tags:          []string{"User"},
		DefaultStatus: 204,
	}, users.RevokeSession(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "change-password",
		Method:      http.MethodPut,
		Path:        "/user/password",
		Summary:     "Change current user password",
		Tags:        []string{"User"},
	}, users.ChangePassword(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "presign-avatar",
		Method:      http.MethodPost,
		Path:        "/user/avatar/presign",
		Summary:     "Get a presigned URL for uploading a profile avatar",
		Tags:        []string{"User"},
	}, users.PresignAvatar(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "confirm-avatar",
		Method:      http.MethodPut,
		Path:        "/user/avatar",
		Summary:     "Confirm a previously uploaded avatar",
		Tags:        []string{"User"},
	}, users.ConfirmAvatar(userDeps))

	huma.Register(api, huma.Operation{
		OperationID: "delete-avatar",
		Method:      http.MethodDelete,
		Path:        "/user/avatar",
		Summary:     "Remove the current avatar",
		Tags:        []string{"User"},
	}, users.DeleteAvatar(userDeps))
}
