// Package admin contains platform-wide administrator endpoints.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
)

type Deps struct {
	Queries *generated.Queries
	Cipher  *secrets.Cipher
	// EnvFallback reports whether the static environment-variable config has a
	// value for a given provider. The frontend uses this to show that the
	// provider is configured even when no DB row exists yet.
	EnvFallback func(provider string) bool
}

const SecretMask = "********"

// --- DTOs ---

type ProviderInfo struct {
	Provider   string    `json:"provider"`
	ClientID   string    `json:"clientId"`
	HasSecret  bool      `json:"hasSecret"`
	Enabled    bool      `json:"enabled"`
	Source     string    `json:"source" doc:"db | env | none"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

type ListProvidersInput struct{}

type ListProvidersOutput struct {
	Body struct {
		Providers []ProviderInfo `json:"providers"`
	}
}

type UpdateProviderInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Body     struct {
		ClientID string `json:"clientId" minLength:"0" maxLength:"255"`
		// Empty string keeps the existing secret. To clear, send "__clear__".
		ClientSecret string `json:"clientSecret" minLength:"0" maxLength:"512"`
		Enabled      bool   `json:"enabled"`
	}
}

type UpdateProviderOutput struct {
	Body ProviderInfo
}

type DeleteProviderInput struct {
	Provider string `path:"provider" enum:"google,line"`
}

type DeleteProviderOutput struct{}

// --- handlers ---

var supportedProviders = []string{"google", "line"}

func ListOAuthProviders(deps Deps) func(context.Context, *ListProvidersInput) (*ListProvidersOutput, error) {
	return func(ctx context.Context, _ *ListProvidersInput) (*ListProvidersOutput, error) {
		out := &ListProvidersOutput{}
		out.Body.Providers = make([]ProviderInfo, 0, len(supportedProviders))
		for _, p := range supportedProviders {
			info := ProviderInfo{Provider: p, Source: "none"}
			row, err := deps.Queries.GetOAuthProviderConfig(ctx, p)
			if err == nil {
				info.ClientID = row.ClientID
				info.HasSecret = len(row.ClientSecretEnc) > 0
				info.Enabled = row.Enabled
				info.UpdatedAt = row.UpdatedAt
				info.Source = "db"
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			} else if deps.EnvFallback != nil && deps.EnvFallback(p) {
				info.HasSecret = true
				info.Enabled = true
				info.Source = "env"
			}
			out.Body.Providers = append(out.Body.Providers, info)
		}
		return out, nil
	}
}

func UpdateOAuthProvider(deps Deps) func(context.Context, *UpdateProviderInput) (*UpdateProviderOutput, error) {
	return func(ctx context.Context, in *UpdateProviderInput) (*UpdateProviderOutput, error) {
		if !deps.Cipher.Available() {
			return nil, apierrors.ToHuma(apierrors.SecretsUnavailable)
		}
		userID, _ := middleware.ActorFromContext(ctx)

		// Look up existing row to decide whether to keep / clear / replace secret.
		existing, err := deps.Queries.GetOAuthProviderConfig(ctx, in.Provider)
		hadRow := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var encSecret []byte
		switch {
		case in.Body.ClientSecret == "" && hadRow:
			encSecret = existing.ClientSecretEnc
		case in.Body.ClientSecret == "":
			// New row but no secret provided: fail rather than create a useless row.
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		case in.Body.ClientSecret == "__clear__":
			encSecret = nil
		default:
			encSecret, err = deps.Cipher.Encrypt([]byte(in.Body.ClientSecret))
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
		}

		_ = hadRow
		updatedBy := sql.NullInt32{Int32: int32(userID), Valid: userID > 0}
		if err := deps.Queries.UpsertOAuthProviderConfig(ctx, generated.UpsertOAuthProviderConfigParams{
			Provider:        in.Provider,
			ClientID:        in.Body.ClientID,
			ClientSecretEnc: encSecret,
			Enabled:         in.Body.Enabled,
			UpdatedBy:       updatedBy,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		row, err := deps.Queries.GetOAuthProviderConfig(ctx, in.Provider)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UpdateProviderOutput{Body: ProviderInfo{
			Provider:  string(row.Provider),
			ClientID:  row.ClientID,
			HasSecret: len(row.ClientSecretEnc) > 0,
			Enabled:   row.Enabled,
			Source:    "db",
			UpdatedAt: row.UpdatedAt,
		}}, nil
	}
}

func DeleteOAuthProvider(deps Deps) func(context.Context, *DeleteProviderInput) (*DeleteProviderOutput, error) {
	return func(ctx context.Context, in *DeleteProviderInput) (*DeleteProviderOutput, error) {
		if err := deps.Queries.DeleteOAuthProviderConfig(ctx, in.Provider); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &DeleteProviderOutput{}, nil
	}
}
