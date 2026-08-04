package users

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
)

// devAccountEmailSuffix restricts dev-login to seeded sample accounts so the
// endpoint can never bypass the password of a real user, even if it is
// accidentally exposed.
const devAccountEmailSuffix = "@example.com"

// DevLogin issues a token for a seeded dev account without checking the
// password. The route is only registered when the API runs in development
// (see config.IsDev / router Deps.DevMode); this handler additionally limits
// itself to @example.com accounts as defense in depth.
func DevLogin(deps Deps) func(context.Context, *DevLoginInput) (*DevLoginOutput, error) {
	return func(ctx context.Context, in *DevLoginInput) (*DevLoginOutput, error) {
		email := strings.ToLower(strings.TrimSpace(in.Body.Email))
		if !strings.HasSuffix(email, devAccountEmailSuffix) {
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		user, err := deps.Queries.GetUserByEmail(ctx, email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, user.ID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &DevLoginOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		out.Body.User = mapUserWithAvatar(ctx, deps, user)
		return out, nil
	}
}
