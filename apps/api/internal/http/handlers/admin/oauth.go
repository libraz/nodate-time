// Package admin contains platform-wide administrator endpoints.
package admin

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
)

type Deps struct {
	// DB opens the transactions the guards need. Revoking an administrator
	// has to hold its count across the write, which a bare Queries cannot do.
	DB      *sql.DB
	Queries *generated.Queries
	Cipher  *secrets.Cipher
	// EnvFallback reports whether the static environment-variable config has a
	// value for a given provider. The frontend uses this to show that the
	// provider is configured even when no DB row exists yet.
	EnvFallback func(provider string) bool
	// AllowedDomains is the configured Google sign-in domain restriction
	// (TC_GOOGLE_ALLOWED_DOMAINS). Empty means unrestricted. Read-only here;
	// surfaced to the admin panel so operators know the active policy.
	AllowedDomains []string
}

const SecretMask = "********"

// --- DTOs ---

type ProviderInfo struct {
	Provider  string     `json:"provider"`
	ClientID  string     `json:"clientId"`
	HasSecret bool       `json:"hasSecret"`
	Enabled   bool       `json:"enabled"`
	Source    string     `json:"source" doc:"db | env | none"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty" required:"false"`
}

type ListProvidersInput struct{}

type ListProvidersOutput struct {
	Body []ProviderInfo
}

// UpdateProviderInput is a merge patch. An absent member keeps what is stored;
// a null clientSecret removes the stored secret. ClientID and Enabled cannot be
// null.
type UpdateProviderInput struct {
	Provider string `path:"provider" enum:"google,line"`
	Body     struct {
		ClientID     patch.Field[string] `json:"clientId,omitzero" required:"false" doc:"at most 255 characters"`
		ClientSecret patch.Field[string] `json:"clientSecret,omitzero" required:"false" doc:"1 to 512 characters; null removes the stored secret"`
		Enabled      patch.Field[bool]   `json:"enabled,omitzero" required:"false"`
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
		out := &ListProvidersOutput{Body: make([]ProviderInfo, 0, len(supportedProviders))}
		for _, p := range supportedProviders {
			info := ProviderInfo{Provider: p, Source: "none"}
			row, err := deps.Queries.GetOAuthProviderConfig(ctx, generated.OauthProviderConfigsProvider(p))
			if err == nil {
				info.ClientID = row.ClientID
				info.HasSecret = len(row.ClientSecretCiphertext) > 0
				info.Enabled = row.Enabled
				info.UpdatedAt = nullTimePtr(row.UpdatedAt)
				info.Source = "db"
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.Internal(ctx, err)
			} else if deps.EnvFallback != nil && deps.EnvFallback(p) {
				info.HasSecret = true
				info.Enabled = true
				info.Source = "env"
			}
			out.Body = append(out.Body, info)
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
		existing, err := deps.Queries.GetOAuthProviderConfig(ctx, generated.OauthProviderConfigsProvider(in.Provider))
		hadRow := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.Internal(ctx, err)
		}

		if in.Body.ClientID.IsNull() || in.Body.Enabled.IsNull() {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		clientID, clientIDSet := in.Body.ClientID.Get()
		if clientIDSet && utf8.RuneCountInString(clientID) > 255 {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		secret, secretSet := in.Body.ClientSecret.Get()
		if secretSet {
			if n := utf8.RuneCountInString(secret); n < 1 || n > 512 {
				return nil, apierrors.ToHuma(apierrors.Validation)
			}
		}

		var encSecret []byte
		switch {
		case secretSet:
			encSecret, err = deps.Cipher.Encrypt([]byte(secret))
			if err != nil {
				return nil, apierrors.Internal(ctx, err)
			}
		case in.Body.ClientSecret.IsNull():
			// Stored as no secret below.
		case hadRow:
			encSecret = existing.ClientSecretCiphertext
		default:
			// New row but no secret provided. Allow it only when the environment
			// supplies the secret for this provider (resolveProvider merges the
			// env secret at runtime), so admins can override clientId/enabled
			// without re-entering an env-managed secret. Otherwise fail rather
			// than create a useless row that can never authenticate.
			if deps.EnvFallback == nil || !deps.EnvFallback(in.Provider) {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
		}

		// An absent member keeps the stored value; a new row starts empty and off.
		if !clientIDSet && hadRow {
			clientID = existing.ClientID
		}
		enabled, enabledSet := in.Body.Enabled.Get()
		if !enabledSet && hadRow {
			enabled = existing.Enabled
		}

		// The column is NOT NULL: no stored secret is an empty one, and nil
		// would be sent as NULL.
		if encSecret == nil {
			encSecret = []byte{}
		}
		// Used only when this inserts; an update leaves the stored id alone.
		pubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		updatedBy := sql.NullInt32{Int32: int32(userID), Valid: userID > 0}
		var row generated.OauthProviderConfig
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpsertOAuthProviderConfig(ctx, generated.UpsertOAuthProviderConfigParams{
				PublicID:               pubID[:],
				Provider:               generated.OauthProviderConfigsProvider(in.Provider),
				ClientID:               clientID,
				ClientSecretCiphertext: encSecret,
				Enabled:                enabled,
				UpdatedByUserID:        updatedBy,
			}); err != nil {
				return err
			}
			var err error
			row, err = q.GetOAuthProviderConfig(ctx, generated.OauthProviderConfigsProvider(in.Provider))
			if err != nil {
				return err
			}
			// The secret is not part of the record, only whether one is stored.
			return appendAudit(ctx, q, auditEntry{
				ActorUserID:      userID,
				Action:           auditOAuthProviderSet,
				ResourceType:     "oauth_provider",
				ResourcePublicID: row.PublicID,
				Payload: map[string]any{
					"provider":  in.Provider,
					"clientId":  row.ClientID,
					"enabled":   row.Enabled,
					"hasSecret": len(row.ClientSecretCiphertext) > 0,
				},
			})
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &UpdateProviderOutput{Body: ProviderInfo{
			Provider:  string(row.Provider),
			ClientID:  row.ClientID,
			HasSecret: len(row.ClientSecretCiphertext) > 0,
			Enabled:   row.Enabled,
			Source:    "db",
			UpdatedAt: nullTimePtr(row.UpdatedAt),
		}}, nil
	}
}

func nullTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	return &n.Time
}

func DeleteOAuthProvider(deps Deps) func(context.Context, *DeleteProviderInput) (*DeleteProviderOutput, error) {
	return func(ctx context.Context, in *DeleteProviderInput) (*DeleteProviderOutput, error) {
		actorID, _ := middleware.ActorFromContext(ctx)
		err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			existing, err := q.GetOAuthProviderConfig(ctx, generated.OauthProviderConfigsProvider(in.Provider))
			if errors.Is(err, sql.ErrNoRows) {
				return apierrors.NotFound
			}
			if err != nil {
				return err
			}
			res, err := q.DeleteOAuthProviderConfig(ctx, generated.OauthProviderConfigsProvider(in.Provider))
			if err != nil {
				return err
			}
			// Report a miss rather than a silent success: an operator who thinks
			// they removed a stored override and did not still has one in effect.
			if changed, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if !changed {
				return apierrors.NotFound
			}
			return appendAudit(ctx, q, auditEntry{
				ActorUserID:      actorID,
				Action:           auditOAuthProviderDelete,
				ResourceType:     "oauth_provider",
				ResourcePublicID: existing.PublicID,
				Payload:          map[string]any{"provider": in.Provider},
			})
		})
		if err != nil {
			var spec *apierrors.Spec
			if errors.As(err, &spec) {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.Internal(ctx, err)
		}
		return &DeleteProviderOutput{}, nil
	}
}
