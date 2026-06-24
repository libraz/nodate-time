package config

import "github.com/caarlos0/env/v11"

type Config struct {
	// Env selects the runtime environment. When set to "development"/"dev" it
	// enables developer conveniences such as the password-less /auth/dev-login
	// endpoint. Defaults to "production" so those are off unless opted in.
	Env                string `env:"TC_ENV" envDefault:"production"`
	Port               int    `env:"TC_PORT" envDefault:"8080"`
	DbDsn              string `env:"TC_DB_DSN" envDefault:"ttuser:ttpw@tcp(127.0.0.1:33306)/timetree_clone?parseTime=true"`
	JWTSecret          string `env:"TC_JWT_SECRET" envDefault:"dev-secret-change-me-in-production"`
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
	return cfg, nil
}

// IsDev reports whether the API is running in a development environment.
func (c *Config) IsDev() bool {
	return c.Env == "development" || c.Env == "dev"
}
