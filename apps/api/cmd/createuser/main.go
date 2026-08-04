// Command createuser inserts a user directly into the database, optionally as a
// platform admin. It is a development/operations helper, not part of the API.
//
// Usage:
//
//	go run ./cmd/createuser -email alice@example.com -password secret123 -name Alice
//	go run ./cmd/createuser -email root@example.com -password secret123 -admin
//
// The database connection comes from TC_DB_DSN (same as the API server).
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
)

func main() {
	email := flag.String("email", "", "email address (required)")
	password := flag.String("password", "", "plaintext password (required, min 8 chars)")
	name := flag.String("name", "", "display name (defaults to the email local part)")
	locale := flag.String("locale", "ja", "BCP 47 locale tag")
	timezone := flag.String("timezone", "Asia/Tokyo", "IANA timezone")
	admin := flag.Bool("admin", false, "grant instance admin rights")
	skipExisting := flag.Bool("skip-existing", false, "exit successfully if the email already exists (for seeding)")
	flag.Parse()

	if err := run(*email, *password, *name, *locale, *timezone, *admin, *skipExisting); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(email, password, name, locale, timezone string, admin, skipExisting bool) error {
	if email == "" || password == "" {
		return errors.New("-email and -password are required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if name == "" {
		name = email
		if at := strings.IndexByte(email, '@'); at > 0 {
			name = email[:at]
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := sql.Open("mysql", cfg.DbDsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect db: %w", err)
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate id: %w", err)
	}

	queries := generated.New(db)

	if skipExisting {
		if _, err := queries.GetUserByEmail(ctx, email); err == nil {
			fmt.Printf("skipped %s (already exists)\n", email)
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("look up existing user: %w", err)
		}
	}

	// The account is spread across four tables and only makes sense as a
	// whole: a user with no identity cannot sign in, one outside the
	// workspace can reach no calendar, and an admin grant without a user is
	// an orphan. One transaction is what keeps a half-created account from
	// existing.
	ws, err := workspace.Ensure(ctx, queries, cfg.WorkspaceSlug, cfg.WorkspaceName, cfg.WorkspaceTimezone, "")
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	var userID uint32
	err = dbtx.Run(ctx, db, func(q *generated.Queries) error {
		res, err := q.CreateUser(ctx, generated.CreateUserParams{
			PublicID:    pubID[:],
			Email:       email,
			DisplayName: name,
			Locale:      locale,
			Timezone:    timezone,
		})
		if err != nil {
			return fmt.Errorf("create user (email may already exist): %w", err)
		}
		insertID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		userID = uint32(insertID)

		identityPubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := q.CreateIdentity(ctx, generated.CreateIdentityParams{
			PublicID:     identityPubID[:],
			UserID:       userID,
			Provider:     generated.IdentitiesProviderLocal,
			Subject:      email,
			PasswordHash: sql.NullString{String: hash, Valid: true},
		}); err != nil {
			return fmt.Errorf("create identity: %w", err)
		}

		memberPubID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.AddWorkspaceMember(ctx, generated.AddWorkspaceMemberParams{
			PublicID:    memberPubID[:],
			WorkspaceID: ws.ID,
			UserID:      userID,
			Role:        generated.WorkspaceMembersRoleMember,
		}); err != nil {
			return fmt.Errorf("add workspace member: %w", err)
		}

		if admin {
			grantPubID, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if err := q.GrantInstanceAdmin(ctx, generated.GrantInstanceAdminParams{
				PublicID: grantPubID[:],
				UserID:   userID,
			}); err != nil {
				return fmt.Errorf("grant instance admin: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	role := "user"
	if admin {
		role = "admin"
	}
	fmt.Printf("created %s (id=%d, public_id=%s, role=%s)\n", email, userID, pubID.String(), role)
	return nil
}
