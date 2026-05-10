package users

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

type Deps struct {
	Queries   *generated.Queries
	JWTSecret string
	Admins    auth.AdminAllowlist
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

func mapUser(u generated.User, admins auth.AdminAllowlist) UserResponse {
	return UserResponse{
		ID:        pubIDToHex(u.PublicID),
		Name:      u.Name,
		Email:     u.Email,
		Icon:      u.Icon,
		Color:     u.Color,
		IsAdmin:   admins.Contains(u.Email),
		CreatedAt: u.CreatedAt,
	}
}

func Register(deps Deps) func(context.Context, *RegisterInput) (*RegisterOutput, error) {
	return func(ctx context.Context, in *RegisterInput) (*RegisterOutput, error) {
		// Check existing
		_, err := deps.Queries.GetUserByEmail(ctx, in.Body.Email)
		if err == nil {
			return nil, apierrors.ToHuma(apierrors.AuthEmailExists)
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
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		insertID, _ := result.LastInsertId()
		token, err := auth.GenerateToken(uint32(insertID), deps.JWTSecret)
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
			IsAdmin:   deps.Admins.Contains(in.Body.Email),
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
				return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if !auth.CheckPassword(in.Body.Password, user.PasswordHash) {
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		token, err := auth.GenerateToken(user.ID, deps.JWTSecret)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &LoginOutput{}
		out.Body.Token = token
		out.Body.User = mapUser(user, deps.Admins)
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
		return &GetMeOutput{Body: mapUser(user, deps.Admins)}, nil
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
		return &UpdateMeOutput{Body: mapUser(user, deps.Admins)}, nil
	}
}
