package users

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// dummyPasswordHash is compared against when a login is attempted for an
// account with no local identity, so the response time does not reveal
// whether the address exists.
var dummyPasswordHash, _ = auth.HashPassword("nodate-time-login-timing-equalizer")

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	JWTSecret   string
	Storage     *storage.Client
	WorkspaceID uint32
	// AllowedDomains restricts which email domains may register a password
	// account, mirroring the Google OIDC policy. Empty means unrestricted.
	AllowedDomains []string
	// Mailer and WebURL deliver the address-confirmation link a registration
	// sends. A nil Mailer leaves the account unconfirmed rather than failing
	// the registration.
	Mailer mailer.Mailer
	WebURL string
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func nullStringValue(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

func mapUser(u generated.User) UserResponse {
	return UserResponse{
		ID:            pubIDToHex(u.PublicID),
		Name:          u.DisplayName,
		Email:         u.Email,
		Locale:        u.Locale,
		Timezone:      u.Timezone,
		EmailVerified: u.EmailVerifiedAt.Valid,
		CreatedAt:     u.CreatedAt,
	}
}

// mapUserWithAvatar is like mapUser but also resolves the avatar URL and the
// instance-admin grant, both of which live outside the user row.
func mapUserWithAvatar(ctx context.Context, deps Deps, u generated.User) UserResponse {
	resp := mapUser(u)
	resp.AvatarURL = avatars.New(deps.Queries, deps.Storage).ForUser(ctx, u)
	if isAdmin, err := deps.Queries.IsInstanceAdmin(ctx, u.ID); err == nil {
		resp.IsAdmin = isAdmin
	}
	return resp
}

// defaultLocale and defaultTimezone seed a new account. Both are
// overridable from the profile; they exist because the columns are NOT NULL
// and a sign-up form does not ask.
const (
	defaultLocale   = "ja"
	defaultTimezone = "Asia/Tokyo"
)

// requestOrigin pulls the client hints stored on a session, so a user can
// later tell their devices apart. Both come from the middleware rather than
// the handler's typed input: a header a handler did not declare is not
// reachable from one.
func requestOrigin(ctx context.Context) (userAgent, ipAddress string) {
	ip, _ := middleware.ClientIPFromContext(ctx)
	ua, _ := middleware.UserAgentFromContext(ctx)
	return ua, ip
}

func Register(deps Deps) func(context.Context, *RegisterInput) (*RegisterOutput, error) {
	return func(ctx context.Context, in *RegisterInput) (*RegisterOutput, error) {
		// Enforce the same access policy as OIDC sign-in.
		allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, in.Body.Email)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if !allowed {
			return nil, apierrors.ToHuma(apierrors.AuthSignupNotAllowed)
		}

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

		userPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		identityPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		var created generated.User
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			// The account and the credential are two rows and must land
			// together: a user with no identity cannot sign in, and an
			// identity with no user is an orphan the unique key will later
			// collide with.
			result, err := q.CreateUser(ctx, generated.CreateUserParams{
				PublicID:    userPubID[:],
				Email:       in.Body.Email,
				DisplayName: in.Body.Name,
				Locale:      defaultLocale,
				Timezone:    defaultTimezone,
			})
			if err != nil {
				return err
			}
			insertID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			userID := uint32(insertID)

			if _, err := q.CreateIdentity(ctx, generated.CreateIdentityParams{
				PublicID:     identityPubID[:],
				UserID:       userID,
				Provider:     generated.IdentitiesProviderLocal,
				Subject:      in.Body.Email,
				PasswordHash: nullString(hash),
			}); err != nil {
				return err
			}

			// Every user joins the single workspace: the contract scopes
			// calendars by it, so an account outside it could reach nothing.
			memberPubID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.AddWorkspaceMember(ctx, generated.AddWorkspaceMemberParams{
				PublicID:    memberPubID[:],
				WorkspaceID: deps.WorkspaceID,
				UserID:      userID,
				Role:        generated.WorkspaceMembersRoleMember,
			}); err != nil {
				return err
			}

			created, err = q.GetUserByID(ctx, userID)
			return err
		})
		if err != nil {
			if isDuplicateKey(err) {
				return nil, apierrors.ToHuma(apierrors.AuthRegisterFailed)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Registration does not prove the registrant reads this mailbox, so the
		// account starts unconfirmed and gets a link that proves it. Sign-in is
		// not held up by this: what an unconfirmed address blocks is provider
		// sign-in linking to the account, not the account itself.
		sendEmailVerification(ctx, deps, created)

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, created.ID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &RegisterOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		out.Body.User = mapUserWithAvatar(ctx, deps, created)
		return out, nil
	}
}

func Login(deps Deps) func(context.Context, *LoginInput) (*LoginOutput, error) {
	return func(ctx context.Context, in *LoginInput) (*LoginOutput, error) {
		identity, err := deps.Queries.GetIdentityByProviderSubject(ctx, generated.GetIdentityByProviderSubjectParams{
			Provider: generated.IdentitiesProviderLocal,
			Subject:  in.Body.Email,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Hash anyway so the response time matches the found path and
				// does not leak which addresses have an account.
				auth.CheckPassword(in.Body.Password, dummyPasswordHash)
				return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// A locked identity is refused before the password is even checked,
		// so brute-forcing cannot proceed by simply continuing to guess.
		if identity.LockedUntilAt.Valid && identity.LockedUntilAt.Time.After(time.Now()) {
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		if !identity.PasswordHash.Valid || !auth.CheckPassword(in.Body.Password, identity.PasswordHash.String) {
			recordFailedAttempt(ctx, deps, identity)
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		user, err := deps.Queries.GetUserByID(ctx, identity.UserID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := deps.Queries.RecordSuccessfulLogin(ctx, identity.ID); err != nil {
			slog.WarnContext(ctx, "failed to clear login lockout", "identityID", identity.ID, "error", err)
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, user.ID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &LoginOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		out.Body.User = mapUserWithAvatar(ctx, deps, user)
		return out, nil
	}
}

// lockoutThreshold and lockoutWindow bound password guessing. The counter
// lives on the identity rather than the user, so locking a password does not
// also lock a provider sign-in that never had one.
const (
	lockoutThreshold = 10
	lockoutWindow    = 15 * time.Minute
)

func recordFailedAttempt(ctx context.Context, deps Deps, identity generated.Identity) {
	lockedUntil := sql.NullTime{}
	if identity.FailedAttempts+1 >= lockoutThreshold {
		lockedUntil = sql.NullTime{Time: time.Now().Add(lockoutWindow), Valid: true}
	}
	if err := deps.Queries.RecordFailedLogin(ctx, generated.RecordFailedLoginParams{
		LockedUntilAt: lockedUntil,
		ID:            identity.ID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to record login attempt", "identityID", identity.ID, "error", err)
	}
}

// Refresh trades a refresh token for a new pair. The old one stops working,
// so a token that leaked cannot be used alongside the real client's -- and
// presenting the spent one ends the session, since only one of the two
// holders can be the person the session belongs to.
func Refresh(deps Deps) func(context.Context, *RefreshInput) (*RefreshOutput, error) {
	return func(ctx context.Context, in *RefreshInput) (*RefreshOutput, error) {
		creds, userID, err := rotateSession(ctx, deps, in.Body.RefreshToken)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errRefreshReplayed) {
				return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &RefreshOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		out.Body.User = mapUserWithAvatar(ctx, deps, user)
		return out, nil
	}
}

// Logout revokes the session the request authenticated with, and only that
// one: signing out of a browser must not sign the user out of their phone.
func Logout(deps Deps) func(context.Context, *LogoutInput) (*LogoutOutput, error) {
	return func(ctx context.Context, _ *LogoutInput) (*LogoutOutput, error) {
		sessionID, ok := middleware.SessionFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		if err := deps.Queries.RevokeSession(ctx, sessionID); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &LogoutOutput{}, nil
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

		identity, err := deps.Queries.GetLocalIdentityByUser(ctx, userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// No local credential to change: this account signs in
				// through a provider.
				return nil, apierrors.ToHuma(apierrors.AuthWrongPassword)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if !identity.PasswordHash.Valid || !auth.CheckPassword(in.Body.CurrentPassword, identity.PasswordHash.String) {
			return nil, apierrors.ToHuma(apierrors.AuthWrongPassword)
		}

		hash, err := auth.HashPassword(in.Body.NewPassword)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Replacing the credential ends every session opened with the old
		// one -- otherwise whoever knew it keeps their access. This device is
		// then given a fresh session, so changing your own password does not
		// sign you out of the browser you did it from.
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdatePasswordHash(ctx, generated.UpdatePasswordHashParams{
				PasswordHash: nullString(hash),
				UserID:       userID,
			}); err != nil {
				return err
			}
			return q.RevokeAllUserSessions(ctx, userID)
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, userID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ChangePasswordOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		return out, nil
	}
}

func UpdateMe(deps Deps) func(context.Context, *UpdateMeInput) (*UpdateMeOutput, error) {
	return func(ctx context.Context, in *UpdateMeInput) (*UpdateMeOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		current, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		timezone := current.Timezone
		if in.Body.Timezone != "" {
			if _, lerr := time.LoadLocation(in.Body.Timezone); lerr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			timezone = in.Body.Timezone
		}
		locale := current.Locale
		if in.Body.Locale != "" {
			locale = in.Body.Locale
		}

		if err := deps.Queries.UpdateUser(ctx, generated.UpdateUserParams{
			DisplayName: in.Body.Name,
			// The uploaded avatar is not touched here: it lives in
			// avatar_storage_object_id, and this endpoint only sets the
			// external URL fallback.
			AvatarURL: current.AvatarURL,
			Timezone:  timezone,
			Locale:    locale,
			ID:        userID,
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UpdateMeOutput{Body: mapUserWithAvatar(ctx, deps, user)}, nil
	}
}
