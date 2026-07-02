package users

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const avatarDownloadTTL = 5 * time.Minute

// dummyPasswordHash is a valid bcrypt hash compared against when a login is
// attempted for a non-existent account, so that the response time does not
// reveal whether the email exists (user-enumeration side channel).
var dummyPasswordHash, _ = auth.HashPassword("nodate-time-login-timing-equalizer")

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

func passwordHashForLogin(passwordHash string) string {
	if strings.HasPrefix(passwordHash, "$2a$") ||
		strings.HasPrefix(passwordHash, "$2b$") ||
		strings.HasPrefix(passwordHash, "$2y$") {
		return passwordHash
	}
	return dummyPasswordHash
}

type Deps struct {
	Queries   *generated.Queries
	JWTSecret string
	Storage   *storage.Client
	// AllowedDomains restricts which email domains may register a password
	// account, mirroring the Google OIDC policy. Empty means unrestricted.
	AllowedDomains []string
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

// avatarURLFor returns a short-lived presigned GET URL for the user's avatar,
// or an empty string if no avatar is set or storage is unavailable.
func avatarURLFor(ctx context.Context, deps Deps, u generated.User) string {
	if deps.Storage == nil || !u.AvatarStorageKey.Valid || u.AvatarStorageKey.String == "" {
		return ""
	}
	url, err := deps.Storage.PresignGet(ctx, u.AvatarStorageKey.String, avatarDownloadTTL)
	if err != nil {
		slog.WarnContext(ctx, "failed to presign avatar URL", "userID", u.ID, "error", err)
		return ""
	}
	return url
}

func mapUser(u generated.User) UserResponse {
	return UserResponse{
		ID:        pubIDToHex(u.PublicID),
		Name:      u.Name,
		Email:     u.Email,
		Icon:      u.Icon,
		Color:     u.Color,
		IsAdmin:   u.IsAdmin,
		CreatedAt: u.CreatedAt,
	}
}

// mapUserWithAvatar is like mapUser but also fills AvatarURL via presigned GET.
func mapUserWithAvatar(ctx context.Context, deps Deps, u generated.User) UserResponse {
	resp := mapUser(u)
	resp.AvatarURL = avatarURLFor(ctx, deps, u)
	return resp
}

func Register(deps Deps) func(context.Context, *RegisterInput) (*RegisterOutput, error) {
	return func(ctx context.Context, in *RegisterInput) (*RegisterOutput, error) {
		// Enforce the same access policy as Google OIDC sign-in.
		allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, in.Body.Email)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if !allowed {
			return nil, apierrors.ToHuma(apierrors.AuthSignupNotAllowed)
		}

		// Check existing
		_, err = deps.Queries.GetUserByEmail(ctx, in.Body.Email)
		if err == nil {
			return nil, apierrors.ToHuma(apierrors.AuthRegisterFailed)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		hash, err := auth.HashPassword(in.Body.Password)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		pubID, _ := uuid.NewV7()
		result, err := deps.Queries.CreateUser(ctx, generated.CreateUserParams{
			PublicID:     pubID[:],
			Name:         in.Body.Name,
			Email:        in.Body.Email,
			Icon:         "👤",
			Color:        "#42A5F5",
			PasswordHash: hash,
		})
		if err != nil {
			if isDuplicateKey(err) {
				return nil, apierrors.ToHuma(apierrors.AuthRegisterFailed)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		insertID, _ := result.LastInsertId()
		token, err := auth.GenerateToken(uint32(insertID), 1, deps.JWTSecret)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &RegisterOutput{}
		out.Body.Token = token
		out.Body.User = UserResponse{
			ID:        pubID.String(),
			Name:      in.Body.Name,
			Email:     in.Body.Email,
			Icon:      "👤",
			Color:     "#42A5F5",
			IsAdmin:   false,
			CreatedAt: time.Now(),
		}
		return out, nil
	}
}

func Login(deps Deps) func(context.Context, *LoginInput) (*LoginOutput, error) {
	return func(ctx context.Context, in *LoginInput) (*LoginOutput, error) {
		user, err := deps.Queries.GetUserByEmail(ctx, in.Body.Email)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Run a comparison anyway so the response time matches the
				// found-user path and does not leak account existence.
				auth.CheckPassword(in.Body.Password, dummyPasswordHash)
				return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if !auth.CheckPassword(in.Body.Password, passwordHashForLogin(user.PasswordHash)) {
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		token, err := auth.GenerateToken(user.ID, user.TokenVersion, deps.JWTSecret)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &LoginOutput{}
		out.Body.Token = token
		out.Body.User = mapUserWithAvatar(ctx, deps, user)
		return out, nil
	}
}

func GetMe(deps Deps) func(context.Context, *GetMeInput) (*GetMeOutput, error) {
	return func(ctx context.Context, _ *GetMeInput) (*GetMeOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &GetMeOutput{Body: mapUserWithAvatar(ctx, deps, user)}, nil
	}
}

func ChangePassword(deps Deps) func(context.Context, *ChangePasswordInput) (*ChangePasswordOutput, error) {
	return func(ctx context.Context, in *ChangePasswordInput) (*ChangePasswordOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}

		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if !auth.CheckPassword(in.Body.CurrentPassword, user.PasswordHash) {
			return nil, apierrors.ToHuma(apierrors.AuthWrongPassword)
		}

		hash, err := auth.HashPassword(in.Body.NewPassword)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		err = deps.Queries.UpdateUserPassword(ctx, generated.UpdateUserPasswordParams{
			PasswordHash: hash,
			ID:           userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &ChangePasswordOutput{}, nil
	}
}

func UpdateMe(deps Deps) func(context.Context, *UpdateMeInput) (*UpdateMeOutput, error) {
	return func(ctx context.Context, in *UpdateMeInput) (*UpdateMeOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		err := deps.Queries.UpdateUser(ctx, generated.UpdateUserParams{
			Name:  in.Body.Name,
			Icon:  in.Body.Icon,
			Color: in.Body.Color,
			ID:    userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UpdateMeOutput{Body: mapUserWithAvatar(ctx, deps, user)}, nil
	}
}
