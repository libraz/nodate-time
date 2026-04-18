package config

import "github.com/caarlos0/env/v11"

type Config struct {
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
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
