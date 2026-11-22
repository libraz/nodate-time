package apppasswords

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/davprofile"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// ProfilePath is where a profile link points, below the API's public URL.
const ProfilePath = "/auth/app-password-profiles/"

// ProfileLinkResponse is a link that installs a CalDAV account on a device.
type ProfileLinkResponse struct {
	URL       string    `json:"url" doc:"Opens the configuration profile. It works like a password until it expires"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CreateProfileLinkInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" doc:"A label for the device the password is for"`
	}
}

type CreateProfileLinkOutput struct {
	Body ProfileLinkResponse
}

type GetProfileInput struct {
	Ticket string `path:"ticket"`
}

type GetProfileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	CacheControl       string `header:"Cache-Control"`
	Body               []byte
}

// CreateProfileLink issues a link to a configuration profile. No password
// exists until the link is opened, but the account is checked now so the
// caller hears why it would be refused.
func CreateProfileLink(deps Deps) func(context.Context, *CreateProfileLinkInput) (*CreateProfileLinkOutput, error) {
	return func(ctx context.Context, in *CreateProfileLinkInput) (*CreateProfileLinkOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		sessionID, ok := middleware.SessionFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		name := strings.TrimSpace(in.Body.Name)
		if name == "" || len([]rune(name)) > 100 {
			return nil, apierrors.ToHuma(apierrors.Validation)
		}
		if err := checkUser(ctx, deps, userID); err != nil {
			return nil, respondCreate(ctx, err, apierrors.AuthTokenInvalid)
		}
		if err := checkLimit(ctx, deps, userID); err != nil {
			return nil, respondCreate(ctx, err, apierrors.AuthTokenInvalid)
		}

		raw, err := deps.Queries.GetSessionPublicIDByID(ctx, sessionID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		session, err := uuid.FromBytes(raw)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		claims, err := NewProfileClaims(session, name)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		now := time.Now()
		ticket, err := deps.ProfileKeys.Mint(claims, now)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &CreateProfileLinkOutput{Body: ProfileLinkResponse{
			URL:       strings.TrimRight(deps.APIPublicURL, "/") + ProfilePath + ticket,
			ExpiresAt: linkExpiry(now),
		}}, nil
	}
}

// GetProfile answers a profile link with the configuration profile. The
// first open stores the password; later opens hand out the same one until a
// device has signed in with it.
func GetProfile(deps Deps) func(context.Context, *GetProfileInput) (*GetProfileOutput, error) {
	return func(ctx context.Context, in *GetProfileInput) (*GetProfileOutput, error) {
		expired := apierrors.ToHuma(apierrors.AppPasswordProfileLinkExpired)
		claims, err := deps.ProfileKeys.Open(in.Ticket, time.Now())
		if err != nil {
			return nil, expired
		}
		session, err := deps.Queries.GetLiveSession(ctx, claims.SessionID[:])
		if errors.Is(err, sql.ErrNoRows) {
			return nil, expired
		}
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		userID := session.UserID
		plain, prefix, hash := deps.ProfileKeys.password(claims)

		// A second pass follows a concurrent open that stored the row first.
		for attempt := 0; ; attempt++ {
			row, err := deps.Queries.GetDavAppPasswordByPublicIDAny(ctx, generated.GetDavAppPasswordByPublicIDAnyParams{
				PublicID: claims.TicketID[:], WorkspaceID: deps.WorkspaceID, UserID: userID,
			})
			if err == nil {
				if !row.Enabled || row.RevokedAt.Valid || row.LastUsedAt.Valid ||
					subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(hash)) != 1 {
					return nil, expired
				}
				if err := checkUser(ctx, deps, userID); err != nil {
					return nil, respondCreate(ctx, err, apierrors.AppPasswordProfileLinkExpired)
				}
				break
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, apierrors.Internal(ctx, err)
			}
			if attempt > 0 {
				return nil, apierrors.Internal(ctx, errors.New("app password row missing after a duplicate key"))
			}
			err = createPassword(ctx, deps, userID, claims.Name, claims.TicketID[:], plain, prefix, hash)
			if err == nil {
				break
			}
			if !isDuplicateKey(err) {
				return nil, respondCreate(ctx, err, apierrors.AppPasswordProfileLinkExpired)
			}
		}

		user, err := deps.Queries.GetUserByID(ctx, userID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		userPub, err := uuid.FromBytes(user.PublicID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &GetProfileOutput{
			ContentType:        davprofile.ContentType,
			ContentDisposition: storage.ContentDisposition(deps.AppName + ".mobileconfig"),
			CacheControl:       "no-store",
			Body: davprofile.Build(davprofile.Params{
				AppName:     deps.AppName,
				DeviceName:  claims.Name,
				Email:       user.Email,
				Password:    plain,
				PublicURL:   deps.APIPublicURL,
				ProfileUUID: claims.TicketID,
				UserUUID:    userPub,
			}),
		}, nil
	}
}
