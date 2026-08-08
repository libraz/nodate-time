// Package workspace resolves the single workspace this deployment runs in.
//
// The shared schema scopes every row by workspace because a second product
// on the same database will not be single-tenant. This application is: it
// has no tenant picker and no way to name a workspace in a request, so one
// row is created at startup and every handler writes under its id.
//
// Keeping the column real rather than defaulting it to a constant is what
// lets the same database later hold a multi-tenant writer without a
// migration -- and what keeps a query that forgets the scope from silently
// matching another product's rows.
package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
)

// Scope is the resolved workspace every request operates in.
type Scope struct {
	ID       uint32
	PublicID []byte
	Slug     string
	Timezone string
}

// Ensure creates the workspace on first start and returns it on every
// later one. The slug is the identity: restarting with the same slug finds
// the same rows, and changing it in configuration points the deployment at
// a different (initially empty) workspace rather than renaming this one.
func Ensure(ctx context.Context, q *generated.Queries, slug, name, timezone, country string) (Scope, error) {
	if slug == "" {
		return Scope{}, errors.New("workspace slug must not be empty")
	}
	if name == "" {
		name = slug
	}
	if timezone == "" {
		timezone = "UTC"
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return Scope{}, fmt.Errorf("generate workspace id: %w", err)
	}
	// EnsureWorkspace is an upsert keyed on slug, so a concurrent start
	// cannot produce two workspaces; the generated public_id above is
	// discarded when the row already exists.
	//
	// Concurrent starts aim every one of those upserts at the same row, which
	// is the shape InnoDB resolves by rolling one side back. The loser is not
	// wrong and has nothing else to do about it -- a caller here either fails
	// to boot or fails a test suite's setup -- so the retry belongs at this
	// call rather than in a return the callers cannot act on.
	if err := retryOnDeadlock(ctx, func() error {
		_, err := q.EnsureWorkspace(ctx, generated.EnsureWorkspaceParams{
			PublicID: pubID[:],
			Slug:     slug,
			Name:     name,
			Timezone: timezone,
			Country:  nullString(country),
		})
		return err
	}); err != nil {
		return Scope{}, fmt.Errorf("ensure workspace %q: %w", slug, err)
	}

	// Read the row back rather than trusting LastInsertId: on the
	// already-exists path the upsert reports no insert, and the id that
	// matters is the stored one.
	ws, err := q.GetWorkspaceBySlug(ctx, slug)
	if err != nil {
		return Scope{}, fmt.Errorf("load workspace %q: %w", slug, err)
	}
	return Scope{ID: ws.ID, PublicID: ws.PublicID, Slug: ws.Slug, Timezone: ws.Timezone}, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
