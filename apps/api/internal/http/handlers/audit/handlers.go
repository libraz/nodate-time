// Package audit exposes read endpoints for the calendar audit log: per-entity
// history and a calendar-wide activity feed.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	auditlog "github.com/libraz/nodate-time/apps/api/internal/audit"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// Deps holds the dependencies shared by the audit read handlers.
type Deps struct {
	DB      *sql.DB
	Queries *generated.Queries
	Storage *storage.Client
}

// actorAvatarTTL bounds the presigned avatar URL lifetime; feed responses are
// short-lived in the client, so an hour comfortably outlives a render.
const actorAvatarTTL = time.Hour

// defaultActivityLimit and maxActivityLimit bound the activity feed page size.
const (
	defaultActivityLimit = 50
	maxActivityLimit     = 200
)

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return ""
	}
	return u.String()
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

// resolveCalendar resolves the calendar by public ID and verifies the caller is
// a member, returning the calendar on success.
func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	pubBytes, err := parseUUID(calPubID)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, apierrors.InternalUnexpected
	}
	if _, err := deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	}); err != nil {
		return generated.Calendar{}, apierrors.CalendarAccessDenied
	}
	return cal, nil
}

// resolveActor builds the actor identity from the joined audit row fields,
// returning nil when the actor user no longer exists (public ID not valid).
func resolveActor(ctx context.Context, deps Deps, publicID, name, icon, avatarKey sql.NullString) *ActorBrief {
	if !publicID.Valid {
		return nil
	}
	a := &ActorBrief{
		ID:   pubIDToHex([]byte(publicID.String)),
		Name: name.String,
		Icon: icon.String,
	}
	if deps.Storage != nil && avatarKey.Valid && avatarKey.String != "" {
		if url, err := deps.Storage.PresignGet(ctx, avatarKey.String, actorAvatarTTL); err == nil {
			a.AvatarURL = url
		}
	}
	return a
}

// EventHistory returns the audit history for a single event.
func EventHistory(deps Deps) func(context.Context, *EventHistoryInput) (*EventHistoryOutput, error) {
	return func(ctx context.Context, in *EventHistoryInput) (*EventHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		if _, err := resolveCalendar(ctx, deps, in.CalendarID, userID); err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		entityPub, err := parseUUID(in.EventID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.EventNotFound)
		}

		rows, err := deps.Queries.ListAuditByEntity(ctx, generated.ListAuditByEntityParams{
			EntityType:     auditlog.EntityEvent,
			EntityPublicID: entityPub,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &EventHistoryOutput{Body: make([]HistoryItem, 0, len(rows))}
		for _, r := range rows {
			out.Body = append(out.Body, HistoryItem{
				ID:        r.ID,
				Action:    r.Action,
				Summary:   r.Summary,
				CreatedAt: r.CreatedAt,
				Actor:     resolveActor(ctx, deps, r.ActorPublicID, r.ActorName, r.ActorIcon, r.ActorAvatarKey),
			})
		}
		return out, nil
	}
}

// MemoHistory returns the audit history for a single memo.
func MemoHistory(deps Deps) func(context.Context, *MemoHistoryInput) (*MemoHistoryOutput, error) {
	return func(ctx context.Context, in *MemoHistoryInput) (*MemoHistoryOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		if _, err := resolveCalendar(ctx, deps, in.CalendarID, userID); err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		entityPub, err := parseUUID(in.MemoID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.MemoNotFound)
		}

		rows, err := deps.Queries.ListAuditByEntity(ctx, generated.ListAuditByEntityParams{
			EntityType:     auditlog.EntityMemo,
			EntityPublicID: entityPub,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &MemoHistoryOutput{Body: make([]HistoryItem, 0, len(rows))}
		for _, r := range rows {
			out.Body = append(out.Body, HistoryItem{
				ID:        r.ID,
				Action:    r.Action,
				Summary:   r.Summary,
				CreatedAt: r.CreatedAt,
				Actor:     resolveActor(ctx, deps, r.ActorPublicID, r.ActorName, r.ActorIcon, r.ActorAvatarKey),
			})
		}
		return out, nil
	}
}

// Activity returns the calendar-wide activity feed, newest first.
func Activity(deps Deps) func(context.Context, *ActivityInput) (*ActivityOutput, error) {
	return func(ctx context.Context, in *ActivityInput) (*ActivityOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		limit := in.Limit
		if limit <= 0 {
			limit = defaultActivityLimit
		}
		if limit > maxActivityLimit {
			limit = maxActivityLimit
		}

		rows, err := deps.Queries.ListAuditByCalendar(ctx, generated.ListAuditByCalendarParams{
			CalendarID: cal.ID,
			Limit:      int32(limit),
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ActivityOutput{Body: make([]FeedItem, 0, len(rows))}
		for _, r := range rows {
			out.Body = append(out.Body, FeedItem{
				HistoryItem: HistoryItem{
					ID:        r.ID,
					Action:    r.Action,
					Summary:   r.Summary,
					CreatedAt: r.CreatedAt,
					Actor:     resolveActor(ctx, deps, r.ActorPublicID, r.ActorName, r.ActorIcon, r.ActorAvatarKey),
				},
				EntityType: r.EntityType,
				EntityID:   pubIDToHex(r.EntityPublicID),
			})
		}
		return out, nil
	}
}
