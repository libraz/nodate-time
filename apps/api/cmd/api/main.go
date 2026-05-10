package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	_ "github.com/go-sql-driver/mysql"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/cleanup"
	"github.com/libraz/nodate-time/apps/api/internal/config"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

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

	queries := generated.New(db)

	// Background cleanup of expired tokens
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	defer cancelCleanup()
	cleanup.Run(cleanupCtx, queries, 15*time.Minute)

	// Storage (MinIO)
	var storageClient *storage.Client
	if cfg.S3Endpoint != "" {
		sc, err := storage.NewClient(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseSSL)
		if err != nil {
			slog.Warn("failed to create storage client, file uploads disabled", "error", err)
		} else {
			if err := sc.EnsureBucket(context.Background()); err != nil {
				slog.Warn("failed to ensure storage bucket, file uploads disabled", "error", err)
			} else {
				storageClient = sc
				slog.Info("storage connected", "bucket", cfg.S3Bucket)
			}
		}
	}

	// Build app router
	mailerClient := mailer.New()
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

	admins := auth.NewAdminAllowlist(cfg.AdminEmails)
	if admins.Empty() {
		slog.Warn("TC_ADMIN_EMAILS is empty; /admin/* endpoints will reject all requests")
	}

	cipher, err := secrets.New(cfg.SecretsKey)
	if err != nil {
		slog.Error("invalid TC_SECRETS_KEY", "error", err)
		os.Exit(1)
	}
	if cipher == nil {
		slog.Warn("TC_SECRETS_KEY is empty; admin OAuth provider edits will be rejected")
	}

	appRouter := router.Build(router.Deps{
		DB:        db,
		Queries:   queries,
		JWTSecret: cfg.JWTSecret,
		Storage:   storageClient,
		Mailer:    mailerClient,
		WebURL:    cfg.WebURL,
		OAuth:     oauthCfg,
		Admins:    admins,
		Cipher:    cipher,
	})

	// Outer router with global middleware
	outer := chi.NewRouter()
	outer.Use(middleware.SecurityHeaders())
	outer.Use(cors.Handler(cors.Options{
		AllowedOrigins:   strings.Split(cfg.CORSAllowedOrigins, ","),
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	outer.Mount("/", appRouter)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      outer,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutdown error", "error", err)
			os.Exit(1)
		}
		slog.Info("server stopped")
	}
}
