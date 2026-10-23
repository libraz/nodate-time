package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Audit actions, one per instance-wide change an administrator can make.
const (
	auditInstanceAdminRevoke = "instance_admin.revoke"
	auditAllowedEmailCreate  = "allowed_email.create"
	auditAllowedEmailDelete  = "allowed_email.delete"
	auditOAuthProviderSet    = "oauth_provider.update"
	auditOAuthProviderDelete = "oauth_provider.delete"
)

// auditEntry is one instance-wide change.
type auditEntry struct {
	ActorUserID uint32
	Action      string
	// Resource names what changed by type and public id; the id may be nil.
	ResourceType     string
	ResourcePublicID []byte
	// Payload must already be redacted: it is stored as given.
	Payload map[string]any
}

// appendAudit writes the entry. q must be bound to the transaction of the
// change being recorded, so the change cannot commit without its row.
func appendAudit(ctx context.Context, q *generated.Queries, e auditEntry) error {
	pubID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("audit: generate id for %q: %w", e.Action, err)
	}
	var payload json.RawMessage
	if len(e.Payload) > 0 {
		if payload, err = json.Marshal(e.Payload); err != nil {
			return fmt.Errorf("audit: marshal payload for %q: %w", e.Action, err)
		}
	}
	if _, err := q.AppendInstanceAuditLog(ctx, generated.AppendInstanceAuditLogParams{
		PublicID:               pubID[:],
		ActorUserID:            sql.NullInt32{Int32: int32(e.ActorUserID), Valid: e.ActorUserID != 0},
		Action:                 e.Action,
		TargetResourceType:     sql.NullString{String: e.ResourceType, Valid: e.ResourceType != ""},
		TargetResourcePublicID: sql.NullString{String: string(e.ResourcePublicID), Valid: len(e.ResourcePublicID) > 0},
		PayloadJSON:            payload,
		OccurredAt:             time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("audit: append %q: %w", e.Action, err)
	}
	return nil
}
