package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

func registerAuth(g apiGroups, deps Deps) {
	api := g.public
	mailOn := mailer.Configured(deps.Mailer)
	appName := appNameOf(deps)
	userDeps := newUserDeps(deps)

	// Email+password auth (register, login, password reset) is registered
	// only when enabled. Disabling it yields an OAuth/OIDC-only deployment.
	if deps.PasswordLoginEnabled {
		huma.Register(api, huma.Operation{
			OperationID: "register",
			Method:      http.MethodPost,
			Path:        "/auth/register",
			Summary:     "Register a new user",
			Tags:        []string{"Auth"},
		}, users.Register(userDeps))

		huma.Register(api, huma.Operation{
			OperationID: "login",
			Method:      http.MethodPost,
			Path:        "/auth/login",
			Summary:     "Login with email and password",
			Tags:        []string{"Auth"},
		}, users.Login(userDeps))
	}

	// Refresh is registered whichever sign-in method is enabled: a
	// session opened through a provider needs renewing just as much as
	// one opened with a password.
	huma.Register(api, huma.Operation{
		OperationID: "refresh-session",
		Method:      http.MethodPost,
		Path:        "/auth/refresh",
		Summary:     "Exchange a refresh token for a new session",
		Tags:        []string{"Auth"},
	}, users.Refresh(userDeps))

	// Development-only: password-less login for seeded sample accounts.
	// Gated by DevMode alone, so it survives even when password login is
	// disabled — dev verification is never blocked.
	if deps.DevMode {
		huma.Register(api, huma.Operation{
			OperationID: "dev-login",
			Method:      http.MethodPost,
			Path:        "/auth/dev-login",
			Summary:     "Password-less login for seeded dev accounts (development only)",
			Tags:        []string{"Auth"},
		}, users.DevLogin(userDeps))
	}

	// Password reset needs password login and a mailer to carry the link.
	if deps.PasswordLoginEnabled && mailOn {
		resetDeps := users.ResetDeps{DB: deps.DB, Queries: deps.Queries, Mailer: deps.Mailer, WebURL: deps.WebURL, AppName: appName}

		huma.Register(api, huma.Operation{
			OperationID: "request-password-reset",
			Method:      http.MethodPost,
			Path:        "/auth/password-reset/request",
			Summary:     "Request a password reset email",
			Tags:        []string{"Auth"},
		}, users.RequestPasswordReset(resetDeps))

		huma.Register(api, huma.Operation{
			OperationID: "confirm-password-reset",
			Method:      http.MethodPost,
			Path:        "/auth/password-reset/confirm",
			Summary:     "Confirm password reset with token",
			Tags:        []string{"Auth"},
		}, users.ConfirmPasswordReset(resetDeps))
	}

	// Confirming an address is public by design: the link lands in a mail
	// client, which has no session with this API.
	if mailOn {
		huma.Register(api, huma.Operation{
			OperationID: "confirm-email-verification",
			Method:      http.MethodPost,
			Path:        "/auth/verify-email/confirm",
			Summary:     "Confirm an email address with a token",
			Tags:        []string{"Auth"},
		}, users.ConfirmEmailVerification(userDeps))
	}

	oauthDeps := users.OAuthDeps{
		DB:                   deps.DB,
		Queries:              deps.Queries,
		WorkspaceID:          deps.WorkspaceID,
		JWTSecret:            deps.JWTSecret,
		WebURL:               deps.WebURL,
		Config:               deps.OAuth,
		Cipher:               deps.Cipher,
		AllowedDomains:       deps.GoogleAllowedDomains,
		PasswordLoginEnabled: deps.PasswordLoginEnabled,
		MailEnabled:          mailOn,
		Defaults:             deps.AccountDefaults,
	}

	huma.Register(api, huma.Operation{
		OperationID: "list-oauth-providers-public",
		Method:      http.MethodGet,
		Path:        "/auth/oauth/providers",
		Summary:     "List enabled OAuth providers for the login screen",
		Tags:        []string{"Auth"},
	}, users.ListEnabledProviders(oauthDeps))

	huma.Register(api, huma.Operation{
		OperationID: "oauth-start",
		Method:      http.MethodGet,
		Path:        "/auth/oauth/{provider}/start",
		Summary:     "Begin OAuth login flow",
		Tags:        []string{"Auth"},
	}, users.OAuthStart(oauthDeps))

	huma.Register(api, huma.Operation{
		OperationID: "oauth-callback",
		Method:      http.MethodGet,
		Path:        "/auth/oauth/{provider}/callback",
		Summary:     "OAuth callback handler",
		Tags:        []string{"Auth"},
	}, users.OAuthCallback(oauthDeps))
}
