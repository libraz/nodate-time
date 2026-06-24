package config

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
)

// defaultJWTSecret is the development fallback secret. It must never be used in
// production: Validate rejects it when the environment is not development.
const defaultJWTSecret = "dev-secret-change-me-in-production"

type Config struct {
	// Env selects the runtime environment. When set to "development"/"dev" it
	// enables developer conveniences such as the password-less /auth/dev-login
	// endpoint. Defaults to "production" so those are off unless opted in.
	Env       string `env:"TC_ENV" envDefault:"production"`
	Port      int    `env:"TC_PORT" envDefault:"8080"`
	DbDsn     string `env:"TC_DB_DSN" envDefault:"ttuser:ttpw@tcp(127.0.0.1:33306)/timetree_clone?parseTime=true"`
	JWTSecret string `env:"TC_JWT_SECRET" envDefault:"dev-secret-change-me-in-production"`

	// PasswordLoginEnabled controls whether email+password authentication
	// (register, login, password reset) is available. Set to false to allow
	// only OAuth/OIDC sign-in — e.g. Google-only deployments. The development
	// quick login (/auth/dev-login) is independent of this flag and stays
	// available whenever IsDev() is true, so dev verification is never blocked.
	PasswordLoginEnabled bool `env:"TC_PASSWORD_LOGIN_ENABLED" envDefault:"true"`
	// SMTP delivery. When SMTPHost is set, password-reset and invite mails are
	// sent via SMTP; otherwise mails are logged to stdout (development only —
	// Validate rejects an unset host in production).
	SMTPHost           string `env:"TC_SMTP_HOST" envDefault:""`
	SMTPPort           int    `env:"TC_SMTP_PORT" envDefault:"587"`
	SMTPUsername       string `env:"TC_SMTP_USERNAME" envDefault:""`
	SMTPPassword       string `env:"TC_SMTP_PASSWORD" envDefault:""`
	SMTPFrom           string `env:"TC_SMTP_FROM" envDefault:"no-reply@nodate-time.local"`
	CORSAllowedOrigins string `env:"TC_CORS_ALLOWED_ORIGINS" envDefault:"http://localhost:5173,http://127.0.0.1:5173"`
	CookieSecure       bool   `env:"TC_COOKIE_SECURE" envDefault:"false"`
	S3Endpoint         string `env:"TC_S3_ENDPOINT" envDefault:"localhost:9000"`
	S3AccessKey        string `env:"TC_S3_ACCESS_KEY" envDefault:"minioadmin"`
	S3SecretKey        string `env:"TC_S3_SECRET_KEY" envDefault:"minioadmin"`
	S3Bucket           string `env:"TC_S3_BUCKET" envDefault:"nodate-time"`
	S3UseSSL           bool   `env:"TC_S3_USE_SSL" envDefault:"false"`

	WebURL    string `env:"TC_WEB_URL" envDefault:"http://localhost:5173"`
	APIPublic string `env:"TC_API_PUBLIC_URL" envDefault:"http://localhost:8080"`

	GoogleClientID     string `env:"TC_GOOGLE_CLIENT_ID" envDefault:""`
	GoogleClientSecret string `env:"TC_GOOGLE_CLIENT_SECRET" envDefault:""`

	// Comma-separated list of email domains allowed to sign in via Google OIDC
	// (e.g. "example.com,agency.co.jp"). Empty means no restriction: any Google
	// account may sign in. When set, addresses outside these domains are rejected
	// unless individually allow-listed in the admin panel.
	GoogleAllowedDomains string `env:"TC_GOOGLE_ALLOWED_DOMAINS" envDefault:""`

	LINEClientID     string `env:"TC_LINE_CLIENT_ID" envDefault:""`
	LINEClientSecret string `env:"TC_LINE_CLIENT_SECRET" envDefault:""`

	// 32-byte key used to encrypt secrets stored in the DB (e.g. OAuth client
	// secrets). Accepts hex (64 chars) or base64. If empty, admin OAuth config
	// edits are rejected so secrets are never written in plaintext.
	SecretsKey string `env:"TC_SECRETS_KEY" envDefault:""`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate enforces production safety guards. Development conveniences (default
// secrets/credentials, console mailer, permissive CORS) are tolerated only when
// IsDev() is true; in production they are hard errors so the process fails fast
// rather than booting with an insecure default.
func (c *Config) Validate() error {
	if c.IsDev() {
		return nil
	}

	// JWT signing secret must be strong and explicitly set.
	if c.JWTSecret == "" || c.JWTSecret == defaultJWTSecret {
		return fmt.Errorf("TC_JWT_SECRET must be set to a non-default value in production")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("TC_JWT_SECRET must be at least 32 bytes in production")
	}

	// Object storage credentials must not be the well-known MinIO defaults.
	if c.S3Endpoint != "" {
		if c.S3AccessKey == "" || c.S3AccessKey == "minioadmin" ||
			c.S3SecretKey == "" || c.S3SecretKey == "minioadmin" {
			return fmt.Errorf("TC_S3_ACCESS_KEY/TC_S3_SECRET_KEY must be set to non-default values in production")
		}
	}

	// CORS with credentials cannot use a wildcard or empty origin.
	for _, o := range c.CORSAllowedOriginList() {
		if o == "" || o == "*" {
			return fmt.Errorf("TC_CORS_ALLOWED_ORIGINS must list explicit origins (no '*') in production")
		}
	}
	if len(c.CORSAllowedOriginList()) == 0 {
		return fmt.Errorf("TC_CORS_ALLOWED_ORIGINS must be set in production")
	}

	// SMTP must be configured so password-reset / invite mail is actually sent.
	if c.SMTPHost == "" {
		return fmt.Errorf("TC_SMTP_HOST must be set in production (console mailer is development-only)")
	}
	return nil
}

// CORSAllowedOriginList parses CORSAllowedOrigins into a trimmed, non-empty slice.
func (c *Config) CORSAllowedOriginList() []string {
	var out []string
	for _, raw := range strings.Split(c.CORSAllowedOrigins, ",") {
		o := strings.TrimSpace(raw)
		if o != "" {
			out = append(out, o)
		}
	}
	return out
}

// IsDev reports whether the API is running in a development environment.
func (c *Config) IsDev() bool {
	return c.Env == "development" || c.Env == "dev"
}

// GoogleAllowedDomainList parses GoogleAllowedDomains into a normalized slice of
// lowercased domains. An empty result means sign-in is unrestricted.
func (c *Config) GoogleAllowedDomainList() []string {
	var out []string
	for _, raw := range strings.Split(c.GoogleAllowedDomains, ",") {
		d := strings.ToLower(strings.TrimSpace(raw))
		d = strings.TrimPrefix(d, "@")
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}
