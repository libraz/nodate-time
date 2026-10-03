package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/observability"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
	"github.com/libraz/nodate-time/apps/api/internal/workspace"
)

// healthReporter assembles the dependency checks the detailed health report
// runs.
//
// Only the database is required. Running without object storage and running
// without a mail relay are both supported configurations, and a report that
// called either one fatal would take a deployment out of rotation over a
// feature it was never given.
//
// Object storage is reported whenever it is configured. A client that could not
// be built is reported as down rather than left out, which would read as a
// deployment that never asked for storage.
func healthReporter(db *sql.DB, storageClient *storage.Client, storageErr error, cfg *config.Config) *observability.Reporter {
	checks := []observability.Check{{
		Name:     "database",
		Required: true,
		Probe:    db.PingContext,
	}}

	if cfg.S3Endpoint != "" {
		probe := func(context.Context) error { return storageErr }
		if storageClient != nil {
			probe = storageClient.Ping
		}
		checks = append(checks, observability.Check{
			Name:  "object_storage",
			Probe: probe,
		})
	}

	if cfg.SMTPHost != "" {
		relay := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
		// A dial rather than a delivery. It answers the question an operator
		// actually has when a reset link does not arrive -- is the relay
		// reachable from in here -- without putting a message on the wire
		// every time someone reads the report.
		checks = append(checks, observability.Check{
			Name: "mail_relay",
			Probe: func(ctx context.Context) error {
				conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", relay)
				if err != nil {
					return err
				}
				return conn.Close()
			},
		})
	}

	return observability.NewReporter(0, checks...)
}

func main() {
	// Wrapped so that every line logged with a request's context carries that
	// request's identifier, including the ones written deep inside a handler
	// that knows nothing about this.
	slog.SetDefault(slog.New(observability.NewContextHandler(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Database
	db, err := sql.Open("mysql", cfg.DbDsn)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	// Pool saturation is invisible from outside the driver: requests queue
	// there rather than in the server, so the handlers and the CPU both look
	// idle while every request waits.
	if err := observability.RegisterDBStats("main", db); err != nil {
		slog.Warn("failed to export database pool metrics", "error", err)
	}

	queries := generated.New(db)

	// Resolve the workspace before anything can serve a request: every
	// handler scopes its queries by it, and a zero id would quietly match
	// nothing instead of failing.
	bootstrapCtx, cancelBootstrap := context.WithTimeout(context.Background(), 10*time.Second)
	ws, err := workspace.Ensure(bootstrapCtx, queries, cfg.WorkspaceSlug, cfg.WorkspaceName, cfg.WorkspaceTimezone, cfg.WorkspaceCountry)
	cancelBootstrap()
	if err != nil {
		slog.Error("failed to resolve workspace", "slug", cfg.WorkspaceSlug, "error", err)
		os.Exit(1)
	}
	slog.Info("workspace ready", "slug", ws.Slug, "timezone", ws.Timezone)

	// Object storage, reached only from the private network. The bytes a
	// browser reads and writes are relayed by this API through signed URLs
	// pointing back at it, so the signer needs the address the API is public
	// at -- and the same signer has to both mint and check them.
	blobSigner := storage.NewSigner(cfg.APIPublic, []byte(cfg.JWTSecret))

	// Lives as long as the process; the background work below stops with it.
	backgroundCtx, cancelBackground := context.WithCancel(context.Background())
	defer cancelBackground()

	// Only a configuration the client library rejects disables storage. An
	// object store that is not answering yet -- it routinely starts alongside
	// this process -- is retried in the background, and the routes and the
	// sweeps keep the client so they work as soon as it does.
	var storageClient *storage.Client
	var storageErr error
	if cfg.S3Endpoint != "" {
		storageClient, storageErr = storage.NewClient(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseSSL, blobSigner)
		if storageErr != nil {
			slog.Error("invalid object storage configuration, file uploads disabled", "error", storageErr)
		} else {
			go ensureBucket(backgroundCtx, storageClient.EnsureBucket, cfg.S3Bucket, 5*time.Second, time.Minute)
		}
	}

	// Background cleanup of expired tokens and abandoned upload rows/objects.
	cleanup.Run(backgroundCtx, queries, storageClient, 15*time.Minute)

	// Build app router. Mail goes out through a dispatcher so a slow relay
	// costs a background goroutine instead of the request that triggered it.
	mailerClient := mailer.NewDispatcher(mailer.New(mailer.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
	}, cfg.IsDev()), mailer.DispatcherOptions{})
	oauthCfg := users.OAuthConfig{
		RedirectBase: cfg.APIPublic,
		Google: users.OAuthProviderConfig{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
			UserinfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
			Scopes:       "openid email profile",
		},
		LINE: users.OAuthProviderConfig{
			ClientID:     cfg.LINEClientID,
			ClientSecret: cfg.LINEClientSecret,
			AuthURL:      "https://access.line.me/oauth2/v2.1/authorize",
			TokenURL:     "https://api.line.me/oauth2/v2.1/token",
			UserinfoURL:  "https://api.line.me/oauth2/v2.1/userinfo",
			Scopes:       "openid profile email",
		},
	}

	cipher, err := secrets.New(cfg.SecretsKey)
	if err != nil {
		slog.Error("invalid TC_SECRETS_KEY", "error", err)
		os.Exit(1)
	}
	if cipher == nil {
		slog.Warn("TC_SECRETS_KEY is empty; admin OAuth provider edits will be rejected")
	}

	appHandler := app.NewHandler(router.Deps{
		DB:                db,
		Queries:           queries,
		JWTSecret:         cfg.JWTSecret,
		WorkspaceID:       ws.ID,
		WorkspacePublicID: ws.PublicID,
		Storage:           storageClient,
		Signer:            blobSigner,
		Mailer:            mailerClient,
		WebURL:            cfg.WebURL,
		AppName:           cfg.AppName,
		AccountDefaults: users.AccountDefaults{
			Locale:   cfg.DefaultLocale,
			Timezone: cfg.WorkspaceTimezone,
		},
		APIPublicURL:         cfg.APIPublic,
		OAuth:                oauthCfg,
		Cipher:               cipher,
		GoogleAllowedDomains: cfg.GoogleAllowedDomainList(),
		DevMode:              cfg.IsDev(),
		PasswordLoginEnabled: cfg.PasswordLoginEnabled,
		TrustedProxies:       cfg.TrustedProxyList(),
		AuthRateLimit:        cfg.AuthRateLimit,
		ShareRateLimit:       cfg.ShareRateLimit,
	}, app.Options{
		CORSAllowedOrigins: cfg.CORSAllowedOriginList(),
		AccessLogger:       slog.Default(),
		TrustedProxies:     cfg.TrustedProxyList(),
	})
	if domains := cfg.GoogleAllowedDomainList(); len(domains) > 0 {
		slog.Info("sign-up and sign-in restricted to email domains", "domains", domains)
	} else {
		slog.Info("sign-up and sign-in are not restricted by email domain")
	}
	if !cfg.PasswordLoginEnabled {
		slog.Info("password login disabled; only OAuth/OIDC sign-in is available")
	}
	if cfg.IsDev() {
		slog.Warn("development mode enabled; /auth/dev-login is exposed (password-less login for @example.com accounts)")
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := app.NewServer(addr, appHandler, app.DefaultTimeouts())

	// Graceful shutdown
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// The administrative listener is a separate server on a separate address
	// so that what it serves cannot be reached through the public one by any
	// routing mistake -- there is no rule to get wrong, only a socket that is
	// not published.
	var adminSrv *http.Server
	if cfg.MetricsEnabled() {
		adminSrv = observability.NewAdminServer(cfg.MetricsAddr, observability.NewAdminHandler(observability.AdminOptions{
			AllowedPeers: cfg.MetricsAllowedPeerList(),
			Health:       healthReporter(db, storageClient, storageErr, cfg),
		}))
		go func() {
			slog.Info("metrics listener starting", "addr", cfg.MetricsAddr, "allowed", cfg.MetricsAllowedCIDRs)
			// A failure here is reported and survived. Losing the ability to
			// observe the process is a bad afternoon; refusing to serve the
			// product over it is an outage.
			if err := adminSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics listener stopped", "addr", cfg.MetricsAddr, "error", err)
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig)
		var servers []shutdowner
		if adminSrv != nil {
			servers = append(servers, adminSrv)
		}
		if err := shutdown(append(servers, srv), mailerClient.Wait); err != nil {
			os.Exit(1)
		}
		slog.Info("server stopped")
	}
}

// The shutdown budgets. compose.prod.yml's stop_grace_period has to cover
// their sum, or the orchestrator kills the process mid-drain.
const (
	httpDrainBudget = 10 * time.Second
	mailDrainBudget = 5 * time.Second
)

type shutdowner interface {
	Shutdown(context.Context) error
}

// shutdown drains the servers in order under one shared deadline, then gives
// mail already handed to the dispatcher its own budget whatever the servers
// did. It reports whether any server failed to drain.
//
// The administrative listener goes first and is not waited on beyond the
// shared deadline: an in-progress CPU profile would otherwise hold the process
// open for as long as the profile was asked to run. Mail is bounded too: an
// undelivered link is recoverable by asking for another one, a process that
// will not exit is not.
func shutdown(servers []shutdowner, waitMail func()) error {
	ctx, cancel := context.WithTimeout(context.Background(), httpDrainBudget)
	defer cancel()
	var failed error
	for _, s := range servers {
		if err := s.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
			failed = err
		}
	}

	drained := make(chan struct{})
	go func() {
		waitMail()
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(mailDrainBudget):
		slog.Warn("shutting down with mail still in flight")
	}
	return failed
}

// ensureBucket creates the bucket once the object store answers, retrying with
// a backoff from first up to ceiling until it succeeds or ctx ends.
func ensureBucket(ctx context.Context, ensure func(context.Context) error, bucket string, first, ceiling time.Duration) {
	wait := first
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := ensure(attemptCtx)
		cancel()
		if err == nil {
			slog.Info("storage connected", "bucket", bucket)
			return
		}
		slog.Warn("object storage not ready, retrying", "bucket", bucket, "retry_in", wait.String(), "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, ceiling)
	}
}
