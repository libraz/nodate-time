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
)

func main() {
	email := flag.String("email", "", "email address (required)")
	password := flag.String("password", "", "plaintext password (required, min 8 chars)")
	name := flag.String("name", "", "display name (defaults to the email local part)")
	icon := flag.String("icon", "👤", "emoji icon")
	color := flag.String("color", "#42A5F5", "hex color")
	admin := flag.Bool("admin", false, "grant platform admin rights (is_admin = 1)")
	skipExisting := flag.Bool("skip-existing", false, "exit successfully if the email already exists (for seeding)")
	flag.Parse()

	if err := run(*email, *password, *name, *icon, *color, *admin, *skipExisting); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(email, password, name, icon, color string, admin, skipExisting bool) error {
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

	res, err := queries.CreateUserWithRole(ctx, generated.CreateUserWithRoleParams{
		PublicID:     pubID[:],
		Name:         name,
		Email:        email,
		Icon:         icon,
		Color:        color,
		PasswordHash: hash,
		IsAdmin:      admin,
	})
	if err != nil {
		return fmt.Errorf("create user (email may already exist): %w", err)
	}

	id, _ := res.LastInsertId()
	role := "user"
	if admin {
		role = "admin"
	}
	fmt.Printf("created %s (id=%d, public_id=%s, role=%s)\n", email, id, pubID.String(), role)
	return nil
}
