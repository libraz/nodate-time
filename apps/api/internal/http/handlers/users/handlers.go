package users

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/patch"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
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
	// AppName is the product the mail names itself as. A rebranded deployment
	// sends mail under its own name rather than the software's.
	AppName string
	// Defaults seed the profile of an account this handler creates.
	Defaults AccountDefaults
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
// instance-admin grant and whether a local password exists, all of which live
// outside the user row.
func mapUserWithAvatar(ctx context.Context, deps Deps, u generated.User) UserResponse {
	resp := mapUser(u)
	resp.AvatarURL = avatars.New(deps.Queries, deps.Storage).ForUser(ctx, u)
	if isAdmin, err := deps.Queries.IsInstanceAdmin(ctx, u.ID); err == nil {
		resp.IsAdmin = isAdmin
	}
	if local, err := deps.Queries.GetLocalIdentityByUser(ctx, u.ID); err == nil {
		resp.HasPassword = local.PasswordHash.Valid && local.PasswordHash.String != ""
	}
	return resp
}

// AccountDefaults seeds an account created without anything to ask: signing
// up collects an address and a password, and signing in through a provider
// collects nothing at all. Both columns are NOT NULL and both values are
// editable from the profile afterwards, so this is where an account starts
// rather than what it is held to.
//
// The deployment supplies them, because the answer is where its people are.
// An installation whose users are not in Japan would otherwise put every
// account nine hours off its own clock and have each of them correct it by
// hand.
type AccountDefaults struct {
	// Locale is a BCP 47 tag; Timezone is an IANA name.
	Locale   string
	Timezone string
}

// The software's own answer, for a caller that named none -- every test
// server, and a deployment that never set the variables. The database refuses
// an empty value in either column, so something has to be here.
const (
	fallbackLocale   = "ja"
	fallbackTimezone = "Asia/Tokyo"
)

func (d AccountDefaults) resolved() AccountDefaults {
	if d.Locale == "" {
		d.Locale = fallbackLocale
	}
	if d.Timezone == "" {
		d.Timezone = fallbackTimezone
	}
	return d
}

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
		in.Body.Email = auth.NormalizeEmail(in.Body.Email)
		// Answered before anything is looked up, because the lookup is what the
		// column cannot do: comparing an address it cannot store is a charset
		// error, which reached the caller as a server fault. The OIDC callback
		// answers the same question with the same helper, so both doors give
		// the same reason.
		if !auth.StorableEmail(in.Body.Email) {
			return nil, apierrors.ToHuma(apierrors.AuthEmailUnsupported)
		}

		// Enforce the same access policy as OIDC sign-in.
		allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, in.Body.Email)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		if !allowed {
			return nil, apierrors.ToHuma(apierrors.AuthSignupNotAllowed)
		}

		_, err = deps.Queries.GetUserByEmail(ctx, in.Body.Email)
		if err == nil {
			return nil, apierrors.ToHuma(apierrors.AuthRegisterFailed)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, apierrors.Internal(ctx, err)
		}

		hash, err := auth.HashPassword(in.Body.Password)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		userPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		identityPubID, err := uuid.NewV7()
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		defaults := deps.Defaults.resolved()
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
				Locale:      defaults.Locale,
				Timezone:    defaults.Timezone,
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
			if _, err := workspace.Join(ctx, q, deps.WorkspaceID, userID); err != nil {
				return err
			}

			created, err = q.GetUserByID(ctx, userID)
			return err
		})
		if err != nil {
			if isDuplicateKey(err) {
				return nil, apierrors.ToHuma(apierrors.AuthRegisterFailed)
			}
			return nil, apierrors.Internal(ctx, fmt.Errorf("registration failed: %w", err))
		}

		// Registration does not prove the registrant reads this mailbox, so the
		// account starts unconfirmed and gets a link that proves it. Sign-in is
		// not held up by this: what an unconfirmed address blocks is provider
		// sign-in linking to the account, not the account itself. A failure is
		// logged rather than returned, since the account already exists and the
		// link can be asked for again.
		if mailer.Configured(deps.Mailer) {
			if err := sendEmailVerification(ctx, deps, deps.Queries, created); err != nil {
				slog.ErrorContext(ctx, "registration confirmation not sent", "userID", created.ID, "error", err)
			}
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, created.ID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.Internal(ctx, fmt.Errorf("registration session not opened: %w", err), "userID", created.ID)
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
		// No account can hold an address the column cannot store, and looking
		// one up is a charset error. Answered like any unknown address.
		email := auth.NormalizeEmail(in.Body.Email)
		if !auth.StorableEmail(email) {
			auth.CheckPassword(in.Body.Password, dummyPasswordHash)
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		// The account is found by the address on the user row and then by its
		// local identity: an identity's subject is the address at creation and
		// is not what a sign-in is matched against.
		user, err := deps.Queries.GetUserByEmail(ctx, email)
		var identity generated.Identity
		if err == nil {
			identity, err = deps.Queries.GetLocalIdentityByUser(ctx, user.ID)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// Hash anyway so the response time matches the found path and
				// does not leak which addresses have an account.
				auth.CheckPassword(in.Body.Password, dummyPasswordHash)
				return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
			}
			return nil, apierrors.Internal(ctx, err)
		}

		// A locked identity is refused whatever the password is, so
		// brute-forcing cannot proceed by simply continuing to guess. The hash
		// is still run: returning without it made the locked path answer in a
		// fraction of the time every other path takes, and that gap says both
		// that the address has an account and that it is currently locked.
		if lockStands(identity, time.Now()) {
			auth.CheckPassword(in.Body.Password, dummyPasswordHash)
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		if !passwordMatches(identity.PasswordHash, in.Body.Password) {
			recordFailedAttempt(ctx, deps, identity)
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		// Admission is decided at every sign-in, not only at registration, so a
		// withdrawn allow-list entry or a narrowed domain list takes effect.
		// Refused as bad credentials so the answer says nothing about the account.
		allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, user.Email)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		if !allowed {
			return nil, apierrors.ToHuma(apierrors.AuthBadCredentials)
		}

		// Asked only once the password is right, so the answer tells nobody
		// without it that the account has a second factor.
		if requiresSecondFactor(identity) {
			return nil, apierrors.ToHuma(apierrors.AuthMFARequired)
		}

		if err := deps.Queries.RecordSuccessfulLogin(ctx, identity.ID); err != nil {
			slog.WarnContext(ctx, "failed to clear login lockout", "identityID", identity.ID, "error", err)
		}

		if err := joinWorkspace(ctx, deps.DB, deps.WorkspaceID, user.ID); err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, user.ID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
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
// also lock a provider sign-in that never had one. Every application on the
// database counts on the same row, so the values are the shared ones
// (sql/core/PROTOCOL.md, Identity rule 4): a looser one here would be the
// limit for all of them.
const (
	lockoutThreshold = 5
	lockoutWindow    = 15 * time.Minute
)

// requiresSecondFactor reports whether the account has confirmed a second
// factor on its local identity. This application cannot verify one, so every
// path that would issue such an account a session refuses it instead
// (sql/core/PROTOCOL.md, Identity rule 1).
func requiresSecondFactor(local generated.Identity) bool {
	return local.MfaConfirmedAt.Valid
}

// userRequiresSecondFactor is requiresSecondFactor for a caller holding the
// user rather than the local identity. An account with no local identity has
// no factor to ask for.
func userRequiresSecondFactor(ctx context.Context, q *generated.Queries, userID uint32) (bool, error) {
	local, err := q.GetLocalIdentityByUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return requiresSecondFactor(local), nil
}

// joinWorkspace adds a signing-in user to the workspace if they have never
// belonged to it, which is how an account another application created comes
// to reach anything here. A removed member stays removed.
func joinWorkspace(ctx context.Context, db *sql.DB, workspaceID, userID uint32) error {
	return dbtx.Run(ctx, db, func(q *generated.Queries) error {
		_, err := workspace.Join(ctx, q, workspaceID, userID)
		return err
	})
}

// passwordMatches compares password against a local identity's hash. An
// identity with no hash still costs one comparison, against the dummy, so
// that path answers no faster than a wrong password and does not reveal the
// account.
func passwordMatches(hash sql.NullString, password string) bool {
	if !hash.Valid {
		auth.CheckPassword(password, dummyPasswordHash)
		return false
	}
	return auth.CheckPassword(password, hash.String)
}

// lockStands reports whether a recorded lock has yet to run out.
func lockStands(identity generated.Identity, now time.Time) bool {
	return identity.LockedUntilAt.Valid && identity.LockedUntilAt.Time.After(now)
}

// recordFailedAttempt hands the threshold and the window to the statement that
// counts the failure: whether the previous window has run out and whether this
// failure reaches the threshold are the same question asked of one row, and
// answering half of it here would put a second clock on it.
func recordFailedAttempt(ctx context.Context, deps Deps, identity generated.Identity) {
	if err := deps.Queries.RecordFailedLogin(ctx, generated.RecordFailedLoginParams{
		LockoutThreshold: lockoutThreshold,
		// Untyped in the generated parameters: sqlc infers nothing for the
		// operand of an INTERVAL, so the window goes over as a plain count of
		// minutes.
		LockoutWindowMinutes: int32(lockoutWindow / time.Minute),
		ID:                   identity.ID,
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
			return nil, apierrors.Internal(ctx, err)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
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
			return nil, apierrors.Internal(ctx, err)
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
			return nil, apierrors.Internal(ctx, err)
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
			return nil, apierrors.Internal(ctx, err)
		}

		// Changing the password hands this device a fresh session, which an
		// account with a second factor cannot be given here.
		if requiresSecondFactor(identity) {
			return nil, apierrors.ToHuma(apierrors.AuthMFARequired)
		}

		// The same counter as Login, so a stolen session cannot guess the
		// current password faster than the sign-in form allows. The caller is
		// already signed in, so saying it is locked tells them nothing new.
		if lockStands(identity, time.Now()) {
			return nil, apierrors.ToHuma(apierrors.RateLimited)
		}
		if !passwordMatches(identity.PasswordHash, in.Body.CurrentPassword) {
			recordFailedAttempt(ctx, deps, identity)
			return nil, apierrors.ToHuma(apierrors.AuthWrongPassword)
		}

		hash, err := auth.HashPassword(in.Body.NewPassword)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
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
			return nil, apierrors.Internal(ctx, err)
		}

		userAgent, ipAddress := requestOrigin(ctx)
		creds, err := startSession(ctx, deps, userID, userAgent, ipAddress)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		out := &ChangePasswordOutput{}
		out.Body.Token = creds.Token
		out.Body.RefreshToken = creds.RefreshToken
		return out, nil
	}
}

// UpdateUser applies a merge patch to the caller's profile.
func UpdateUser(deps Deps) func(context.Context, *UpdateUserInput) (*UpdateUserOutput, error) {
	return func(ctx context.Context, in *UpdateUserInput) (*UpdateUserOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}

		// Nothing here is nullable: a column that cannot be empty has no
		// "clear" to ask for.
		name, nameSet, ok := patchedString(in.Body.Name, 1, maxUserNameLen)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		locale, localeSet, ok := patchedString(in.Body.Locale, 1, maxLocaleLen)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		timezone, timezoneSet, ok := patchedString(in.Body.Timezone, 1, maxTimezoneLen)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		if timezoneSet {
			if _, err := time.LoadLocation(timezone); err != nil {
				return nil, apierrors.ToHuma(apierrors.Validation)
			}
		}

		err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			current, err := q.GetUserByIDForUpdate(ctx, userID)
			if err != nil {
				return err
			}
			params := generated.UpdateUserParams{
				DisplayName: current.DisplayName,
				// The uploaded avatar is not touched here: it lives in
				// avatar_storage_object_id, and the external URL fallback has
				// no member in the patch.
				AvatarURL: current.AvatarURL,
				Timezone:  current.Timezone,
				Locale:    current.Locale,
				ID:        userID,
			}
			if nameSet {
				params.DisplayName = name
			}
			if localeSet {
				params.Locale = locale
			}
			if timezoneSet {
				params.Timezone = timezone
			}
			return q.UpdateUser(ctx, params)
		})
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &UpdateUserOutput{Body: mapUserWithAvatar(ctx, deps, user)}, nil
	}
}

const (
	maxUserNameLen = 100
	maxLocaleLen   = 35
	maxTimezoneLen = 64
)

// patchedString reads a patch member that cannot be null. set reports whether
// it carries a value; ok is false for a null or a length outside [min, max]
// code points.
func patchedString(f patch.Field[string], min, max int) (v string, set, ok bool) {
	if f.IsNull() {
		return "", false, false
	}
	v, set = f.Get()
	if !set {
		return "", false, true
	}
	if n := utf8.RuneCountInString(v); n < min || n > max {
		return "", false, false
	}
	return v, true, true
}
