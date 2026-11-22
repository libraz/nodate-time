// Package apppasswords serves the signed-in user's CalDAV app passwords: a
// per-device credential shown once at creation and revocable on its own.
package apppasswords

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/apppassword"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// MaxLivePasswords is the number of unrevoked app passwords one user may hold.
const MaxLivePasswords = 20

// Deps carries what the handlers need.
type Deps struct {
	Queries     *generated.Queries
	WorkspaceID uint32
	// APIPublicURL is the address clients reach the API at; its host is what a
	// calendar app takes as the server.
	APIPublicURL string
}

// AppPasswordResponse is one live app password. The secret itself is never
// part of it.
type AppPasswordResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix" doc:"First characters of the password, to recognise it in a list"`
	LastUsedAt *time.Time `json:"lastUsedAt" required:"false" nullable:"true"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// CreatedAppPasswordResponse is the one answer that carries the plaintext.
type CreatedAppPasswordResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"`
	CreatedAt time.Time `json:"createdAt"`
	Password  string    `json:"password" doc:"The app password. It is shown only in this response"`
	// ServerHost is the value to enter as the server in a calendar app.
	ServerHost string `json:"serverHost"`
	// Username is the account email, which calendar apps ask for next to the password.
	Username string `json:"username"`
}

type ListInput struct{}

type ListOutput struct {
	Body []AppPasswordResponse
}

type CreateInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" doc:"A label for the device the password is for"`
	}
}

type CreateOutput struct {
	Body CreatedAppPasswordResponse
}

type RevokeInput struct {
	AppPasswordID string `path:"appPasswordId"`
}

type RevokeOutput struct{}

func pubIDString(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func serverHost(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	return u.Host
}

// ListAppPasswords returns the caller's unrevoked app passwords, newest first.
func ListAppPasswords(deps Deps) func(context.Context, *ListInput) (*ListOutput, error) {
	return func(ctx context.Context, _ *ListInput) (*ListOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		rows, err := deps.Queries.ListDavAppPasswords(ctx, generated.ListDavAppPasswordsParams{
			WorkspaceID: deps.WorkspaceID, UserID: userID,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		out := &ListOutput{Body: make([]AppPasswordResponse, 0, len(rows))}
		for _, r := range rows {
			item := AppPasswordResponse{
				ID:        pubIDString(r.PublicID),
				Name:      r.Name,
				Prefix:    r.TokenPrefix,
				CreatedAt: r.CreatedAt,
			}
			if r.LastUsedAt.Valid {
				t := r.LastUsedAt.Time
				item.LastUsedAt = &t
			}
			out.Body = append(out.Body, item)
		}
		return out, nil
	}
}

// CreateAppPassword issues a new app password and returns its plaintext once.
func CreateAppPassword(deps Deps) func(context.Context, *CreateInput) (*CreateOutput, error) {
	return func(ctx context.Context, in *CreateInput) (*CreateOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" || len([]rune(name)) > 100 {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}

		state, err := deps.Queries.GetUserForAppPasswordCreate(ctx, userID)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
			}
			return nil, apierrors.Internal(ctx, err)
		}
		if state.HasLocalIdentity && !state.EmailVerifiedAt.Valid {
			return nil, apierrors.ToHuma(apierrors.AppPasswordEmailUnverified)
		}
		if state.HasLocalMfa {
			return nil, apierrors.ToHuma(apierrors.AppPasswordMFAEnabled)
		}

		live, err := deps.Queries.CountLiveDavAppPasswords(ctx, generated.CountLiveDavAppPasswordsParams{
			WorkspaceID: deps.WorkspaceID, UserID: userID,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		if live >= MaxLivePasswords {
			return nil, apierrors.ToHuma(apierrors.AppPasswordLimitReached)
		}

		plain, prefix, hash, err := apppassword.Generate()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		pub, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		if _, err := deps.Queries.CreateDavAppPassword(ctx, generated.CreateDavAppPasswordParams{
			PublicID:    pub[:],
			WorkspaceID: deps.WorkspaceID,
			UserID:      userID,
			Name:        name,
			TokenHash:   hash,
			TokenPrefix: prefix,
		}); err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		// Re-read for the stored created_at, and the email the apps sign in with.
		rows, err := deps.Queries.ListDavAppPasswords(ctx, generated.ListDavAppPasswordsParams{
			WorkspaceID: deps.WorkspaceID, UserID: userID,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		var created time.Time
		for _, r := range rows {
			if r.TokenHash == hash {
				created = r.CreatedAt
				break
			}
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		return &CreateOutput{Body: CreatedAppPasswordResponse{
			ID:         pub.String(),
			Name:       name,
			Prefix:     prefix,
			CreatedAt:  created,
			Password:   plain,
			ServerHost: serverHost(deps.APIPublicURL),
			Username:   user.Email,
		}}, nil
	}
}

// RevokeAppPassword revokes one of the caller's app passwords.
func RevokeAppPassword(deps Deps) func(context.Context, *RevokeInput) (*RevokeOutput, error) {
	return func(ctx context.Context, in *RevokeInput) (*RevokeOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		pub, err := uuid.Parse(in.AppPasswordID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AppPasswordNotFound)
		}
		res, err := deps.Queries.RevokeDavAppPassword(ctx, generated.RevokeDavAppPasswordParams{
			PublicID: pub[:], WorkspaceID: deps.WorkspaceID, UserID: userID,
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		// Another user's password and one already revoked answer alike.
		if affected == 0 {
			return nil, apierrors.ToHuma(apierrors.AppPasswordNotFound)
		}
		return &RevokeOutput{}, nil
	}
}
