package users

import (
	"context"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

// SessionResponse is one live sign-in as its owner sees it.
//
// A session is the revocation point for everything a device can do, so the
// person it belongs to has to be able to see the list and end any of them. A
// stolen token is otherwise good until it expires, with nothing the victim
// can do but change their password and sign every device out at once.
type SessionResponse struct {
	ID string `json:"id"`
	// Current marks the session this request came in on, so ending a session
	// is not a guess about which row is the browser you are looking at.
	Current   bool      `json:"current"`
	UserAgent string    `json:"userAgent,omitempty"`
	IPAddress string    `json:"ipAddress,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ListSessionsInput struct{}

type ListSessionsOutput struct {
	Body []SessionResponse
}

type RevokeSessionInput struct {
	SessionID string `path:"sessionId"`
}

type RevokeSessionOutput struct{}

// unpackIP renders the packed address a session row stores. An unreadable
// value yields an empty string rather than an error: the list is for
// recognising a device, and one field of it being unavailable is not a reason
// to withhold the rest.
func unpackIP(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	addr, ok := netipAddr(raw)
	if !ok {
		return ""
	}
	return addr
}

func netipAddr(raw []byte) (string, bool) {
	switch len(raw) {
	case net.IPv4len, net.IPv6len:
		return net.IP(raw).String(), true
	default:
		return "", false
	}
}

// ListSessions returns the caller's live sign-ins, newest first.
func ListSessions(deps Deps) func(context.Context, *ListSessionsInput) (*ListSessionsOutput, error) {
	return func(ctx context.Context, _ *ListSessionsInput) (*ListSessionsOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		currentID, _ := middleware.SessionFromContext(ctx)

		rows, err := deps.Queries.ListSessionsForUser(ctx, userID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListSessionsOutput{Body: make([]SessionResponse, 0, len(rows))}
		for _, s := range rows {
			out.Body = append(out.Body, SessionResponse{
				ID:        pubIDToHex(s.PublicID),
				Current:   s.ID == currentID,
				UserAgent: nullStringValue(s.UserAgent),
				IPAddress: unpackIP(s.IpAddress),
				CreatedAt: s.CreatedAt,
				ExpiresAt: s.ExpiresAt,
			})
		}
		return out, nil
	}
}

// RevokeSession ends one of the caller's sign-ins.
func RevokeSession(deps Deps) func(context.Context, *RevokeSessionInput) (*RevokeSessionOutput, error) {
	return func(ctx context.Context, in *RevokeSessionInput) (*RevokeSessionOutput, error) {
		userID, ok := middleware.ActorFromContext(ctx)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthTokenInvalid)
		}
		pub, err := uuid.Parse(in.SessionID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AuthSessionNotFound)
		}
		res, err := deps.Queries.RevokeSessionByPublicID(ctx, generated.RevokeSessionByPublicIDParams{
			PublicID: pub[:],
			UserID:   userID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		// A session that is not the caller's, or is already gone, is reported
		// the same way: the list a person can act on is their own, and saying
		// which of the two it was would answer a question about somebody else.
		if affected == 0 {
			return nil, apierrors.ToHuma(apierrors.AuthSessionNotFound)
		}
		return &RevokeSessionOutput{}, nil
	}
}
