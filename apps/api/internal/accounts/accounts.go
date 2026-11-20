// Package accounts creates user accounts outside the HTTP surface, for the
// operator tools that seed or migrate people into a deployment.
package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
)

// Params describes the account to create.
type Params struct {
	// Email is stored lower-cased and trimmed, as auth.NormalizeEmail returns it.
	Email string
	// Name defaults to the local part of Email.
	Name string
	// Password is optional; without it the account has no local identity and
	// is claimed by the first provider sign-in with a matching verified email.
	Password string
	// Locale and Timezone default to the deployment's values when empty.
	Locale, Timezone string
	// Admin grants instance admin rights.
	Admin bool
	// SkipExisting returns the existing user, untouched, instead of failing.
	SkipExisting bool
	// AllowlistNote is the note on the sign-in allow-list entry written under
	// a restriction.
	AllowlistNote string
}

// Result is the outcome of Create.
type Result struct {
	User generated.User
	// Created is false when the user already existed and SkipExisting was set.
	Created bool
}

// Create writes the account, its workspace membership and, where the
// deployment calls for them, the allow-list entry and admin grant.
func Create(ctx context.Context, db *sql.DB, cfg *config.Config, p Params) (Result, error) {
	email := auth.NormalizeEmail(p.Email)
	// An account this package writes is one the API then has to live with, so
	// the password it accepts is the one /auth/register would have accepted.
	var hash string
	if p.Password != "" {
		if err := auth.ValidatePassword(p.Password); err != nil {
			return Result{}, err
		}
		var err error
		if hash, err = auth.HashPassword(p.Password); err != nil {
			return Result{}, fmt.Errorf("hash password: %w", err)
		}
	}
	name := p.Name
	if name == "" {
		name = p.Email
		if at := strings.IndexByte(p.Email, '@'); at > 0 {
			name = p.Email[:at]
		}
	}
	locale, timezone := p.Locale, p.Timezone
	if locale == "" {
		locale = cfg.DefaultLocale
	}
	if timezone == "" {
		timezone = cfg.WorkspaceTimezone
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return Result{}, fmt.Errorf("generate id: %w", err)
	}

	queries := generated.New(db)

	if p.SkipExisting {
		existing, err := queries.GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			return Result{User: existing, Created: false}, nil
		case !errors.Is(err, sql.ErrNoRows):
			return Result{}, fmt.Errorf("look up existing user: %w", err)
		}
	}

	// The account is spread across several tables and only makes sense as a
	// whole: a user outside the workspace can reach no calendar, and an admin
	// grant without a user is an orphan. One transaction is what keeps a half-created account from
	// existing.
	ws, err := workspace.Ensure(ctx, queries, cfg.WorkspaceSlug, cfg.WorkspaceName, cfg.WorkspaceTimezone, cfg.WorkspaceCountry)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workspace: %w", err)
	}

	var user generated.User
	err = dbtx.Run(ctx, db, func(q *generated.Queries) error {
		res, err := q.CreateUser(ctx, generated.CreateUserParams{
			PublicID:    pubID[:],
			Email:       email,
			DisplayName: name,
			Locale:      locale,
			Timezone:    timezone,
		})
		if err != nil {
			return fmt.Errorf("create user (email may already exist): %w", err)
		}
		insertID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		userID := uint32(insertID)

		if hash != "" {
			identityPubID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if _, err := q.CreateIdentity(ctx, generated.CreateIdentityParams{
				PublicID:     identityPubID[:],
				UserID:       userID,
				Provider:     generated.IdentitiesProviderLocal,
				Subject:      email,
				PasswordHash: sql.NullString{String: hash, Valid: true},
			}); err != nil {
				return fmt.Errorf("create identity: %w", err)
			}
		}

		if _, err := workspace.Join(ctx, q, ws.ID, userID); err != nil {
			return fmt.Errorf("add workspace member: %w", err)
		}

		// Under a restriction, an account the operator created by hand is a
		// deliberate exception to it, so its address joins the allowlist. On
		// an open deployment a row would itself switch the restriction on and
		// close sign-in to everyone else, so none is written.
		restricted := len(cfg.GoogleAllowedDomainList()) > 0
		if !restricted {
			entries, err := q.CountEnabledOauthSigninAllowlistEntries(ctx)
			if err != nil {
				return fmt.Errorf("read allow-list: %w", err)
			}
			restricted = entries > 0
		}
		if restricted {
			allowPubID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.UpsertOauthSigninAllowlistEmail(ctx, generated.UpsertOauthSigninAllowlistEmailParams{
				PublicID:   allowPubID[:],
				EntryValue: email,
				Notes:      sql.NullString{String: p.AllowlistNote, Valid: true},
			}); err != nil {
				return fmt.Errorf("allow-list address: %w", err)
			}
		}

		if p.Admin {
			grantPubID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.GrantInstanceAdmin(ctx, generated.GrantInstanceAdminParams{
				PublicID: grantPubID[:],
				UserID:   userID,
			}); err != nil {
				return fmt.Errorf("grant instance admin: %w", err)
			}
		}

		user, err = q.GetUserByID(ctx, userID)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return Result{User: user, Created: true}, nil
}
