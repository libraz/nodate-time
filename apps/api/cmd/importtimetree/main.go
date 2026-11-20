// Command importtimetree copies shared calendars from TimeTree into the
// database, assigning each event an owner by its label. It is an operations
// helper, not part of the API.
//
// Usage:
//
//	TIMETREE_EMAIL=... TIMETREE_PASSWORD=... \
//	  go run ./cmd/importtimetree -mapping mapping.yaml -dry-run
//	... go run ./cmd/importtimetree -mapping - < mapping.yaml
//
// The TimeTree credentials come from the environment so they never appear in
// the process list. The database connection comes from TC_DB_DSN (same as the
// API server).
//
// Exit codes: 0 success; 1 one or more events failed while writing; 2 nothing
// was written (bad input, credentials, validation, login or fetch).
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/timetree"
)

func main() {
	os.Exit(run())
}

func run() int {
	mappingPath := flag.String("mapping", "", `mapping file path, or "-" for stdin (required)`)
	dryRun := flag.Bool("dry-run", false, "fetch and validate, print the report, write nothing")
	flag.Parse()

	if *mappingPath == "" {
		fmt.Fprintln(os.Stderr, "error: -mapping is required")
		return 2
	}

	var mapping io.Reader = os.Stdin
	if *mappingPath != "-" {
		f, err := os.Open(*mappingPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return 2
		}
		defer f.Close()
		mapping = f
	}

	// This command writes rows; it signs nothing and serves nothing, so the
	// serving configuration's checks would only refuse it over unused values.
	cfg, err := config.LoadForTooling()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: load config:", err)
		return 2
	}

	db, err := sql.Open("mysql", cfg.DbDsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: open db:", err)
		return 2
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		fmt.Fprintln(os.Stderr, "error: connect db:", err)
		return 2
	}

	code, err := timetree.Run(ctx, timetree.Options{
		Email:    os.Getenv("TIMETREE_EMAIL"),
		Password: os.Getenv("TIMETREE_PASSWORD"),
		Mapping:  mapping,
		DryRun:   *dryRun,
		DB:       db,
		Config:   cfg,
		Out:      os.Stdout,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if code == 0 {
			code = 2
		}
	}
	return code
}
