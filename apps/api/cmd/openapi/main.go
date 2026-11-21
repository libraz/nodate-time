// Command openapi writes the API's OpenAPI document without a database.
//
// The document describes every operation a fully configured deployment
// offers: password sign-in on, development endpoints off, mail delivery on.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
)

// deliveringMailer is any mailer other than the noop one, so the mail-only
// operations are documented.
type deliveringMailer struct{}

func (deliveringMailer) Send(context.Context, mailer.Message) error { return nil }

func main() {
	out := flag.String("o", "openapi.json", "output path")
	flag.Parse()

	_, doc := router.BuildResult(router.Deps{
		Mailer:               deliveringMailer{},
		PasswordLoginEnabled: true,
		DevMode:              false,
		AuthRateLimit:        -1,
		ShareRateLimit:       -1,
	})
	body, err := json.Marshal(doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
}
