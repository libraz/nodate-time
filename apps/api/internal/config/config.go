package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
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

	// The single workspace this deployment runs in. The shared schema scopes
	// every row by workspace because a second product on the same database
	// will not be single-tenant; this application is, so it names one at
	// startup and creates it if it is not there.
	//
	// The slug is the identity: pointing a deployment at a different slug
	// gives it a different (initially empty) workspace rather than renaming
	// this one.
	WorkspaceSlug     string `env:"TC_WORKSPACE_SLUG" envDefault:"default"`
	WorkspaceName     string `env:"TC_WORKSPACE_NAME" envDefault:"Nodate Time"`
	WorkspaceTimezone string `env:"TC_WORKSPACE_TIMEZONE" envDefault:"Asia/Tokyo"`
	WorkspaceCountry  string `env:"TC_WORKSPACE_COUNTRY" envDefault:"JP"`

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

	// There is deliberately no cookie-security setting. The only cookie this
	// service sets is the one binding an OAuth flow to the browser that began
	// it, and whether it carries Secure follows from RedirectBase being an
	// https URL -- the same value the browser is sent back to. A separate knob
	// could only ever disagree with that, and the way it would disagree is by
	// promising Secure on a deployment that is not serving over TLS.

	// TrustedProxies lists reverse-proxy hops (comma-separated CIDR or bare IP)
	// allowed to set X-Forwarded-For for per-client rate limiting. Empty (the
	// default) trusts no proxy: RemoteAddr is always used directly, so a
	// deployment behind an unlisted proxy has every request collapse onto one
	// rate-limit bucket rather than risk trusting a spoofable header.
	TrustedProxies string `env:"TC_TRUSTED_PROXIES" envDefault:""`

	// AuthRateLimit is the per-IP per-minute budget for sensitive
	// unauthenticated endpoints (sign-in, registration, password reset,
	// OAuth). Zero uses the built-in default; negative disables the limiter.
	AuthRateLimit int `env:"TC_AUTH_RATE_LIMIT" envDefault:"0"`
	// ShareRateLimit is the same for public share links, counted separately so
	// an audience reading a shared calendar cannot exhaust the sign-in budget.
	ShareRateLimit int    `env:"TC_SHARE_RATE_LIMIT" envDefault:"0"`
	S3Endpoint     string `env:"TC_S3_ENDPOINT" envDefault:"localhost:9000"`
	S3AccessKey    string `env:"TC_S3_ACCESS_KEY" envDefault:"minioadmin"`
	S3SecretKey    string `env:"TC_S3_SECRET_KEY" envDefault:"minioadmin"`
	S3Bucket       string `env:"TC_S3_BUCKET" envDefault:"nodate-time"`
	S3UseSSL       bool   `env:"TC_S3_USE_SSL" envDefault:"false"`

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

	// AllowDefaultObjectStorageCredentials permits the published MinIO
	// credentials. The local object store ships with them and a developer
	// running the compose stack has no other pair to offer, so this is a real
	// need rather than a relaxed rule; anywhere else they are a username and
	// password anyone can look up.
	AllowDefaultObjectStorageCredentials bool `env:"TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS" envDefault:"false"`
	// AllowConsoleMailer permits running with no SMTP relay. Mail is then
	// written to the log rather than delivered, which both prints
	// password-reset links and leaves the reset flow quietly broken.
	AllowConsoleMailer bool `env:"TC_ALLOW_CONSOLE_MAILER" envDefault:"false"`
}

// parse reads the environment and normalizes the DSN. Both loaders need this
// much; they differ only in which guards they then apply.
func parse() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	dsn, err := NormalizeDSN(cfg.DbDsn)
	if err != nil {
		return nil, err
	}
	cfg.DbDsn = dsn
	return cfg, nil
}

// Load reads the configuration for a process that serves the API, and enforces
// every guard such a process owes: see Validate.
func Load() (*Config, error) {
	cfg, err := parse()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.logRelaxations()
	return cfg, nil
}

// LoadForTooling reads the same configuration for a command-line tool that
// talks to the database and serves no requests.
//
// The guards in Validate all describe serving: signing sessions, answering
// cross-origin requests, storing objects, delivering mail. A command that
// writes one row does none of those, and refusing to start it over an SMTP
// host it never reads protects nothing -- it only teaches whoever runs it to
// set every allowance to true, which is the bundle this split exists to undo.
//
// The DSN is still normalized and still has to parse: that is the one piece of
// configuration such a tool does use.
//
// Not for a process that serves requests. Load is for those.
func LoadForTooling() (*Config, error) {
	return parse()
}

// Validate enforces the guards a process serving the API owes, whatever
// environment it runs in.
//
// Every check runs in every environment. TC_ENV used to switch all four off at
// once, which meant a deployment setting it for one reason silently got the
// other three -- including signing every session with a secret published in
// this repository.
//
// Two of the four are answered by a variable of their own, because local
// development has a real need for them: the object store ships with known
// credentials and there is no mail relay. The other two had no such need. A
// development CORS list is already a pair of explicit localhost origins, and a
// development signing secret is as cheap to generate as it is to copy, so
// both are simply required and the exemptions are gone.
//
// TC_ENV still selects the environment, but it now only adds the development
// affordances (/auth/dev-login); it takes no protection away.
func (c *Config) Validate() error {
	// The signing secret is the session system. Its default is committed here,
	// so accepting it anywhere reachable lets anyone mint a token for any
	// account, administrators included.
	if c.JWTSecret == "" || c.JWTSecret == defaultJWTSecret {
		return fmt.Errorf("TC_JWT_SECRET must be set to a value of your own (the built-in default is published in this repository)")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("TC_JWT_SECRET must be at least 32 bytes")
	}

	// The object store's published defaults.
	if c.S3Endpoint != "" && !c.AllowDefaultObjectStorageCredentials {
		if c.S3AccessKey == "" || c.S3AccessKey == "minioadmin" ||
			c.S3SecretKey == "" || c.S3SecretKey == "minioadmin" {
			return fmt.Errorf("TC_S3_ACCESS_KEY/TC_S3_SECRET_KEY are the published defaults; set your own, or set TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS=true to run against a local object store")
		}
	}

	// CORS with credentials cannot use a wildcard or an empty origin. There is
	// deliberately no allowance: the built-in default is already a list of
	// explicit localhost origins, so nothing needs one.
	if len(c.CORSAllowedOriginList()) == 0 {
		return fmt.Errorf("TC_CORS_ALLOWED_ORIGINS must be set")
	}
	for _, o := range c.CORSAllowedOriginList() {
		if o == "" || o == "*" {
			return fmt.Errorf("TC_CORS_ALLOWED_ORIGINS must list explicit origins (no '*')")
		}
	}

	// Without a relay, mail is written to the log instead of delivered.
	if c.SMTPHost == "" && !c.AllowConsoleMailer {
		return fmt.Errorf("TC_SMTP_HOST must be set, or set TC_ALLOW_CONSOLE_MAILER=true to write mail to the log instead of delivering it")
	}
	return nil
}

// logRelaxations names every protection this process is running without, and
// the variable that turned it off.
//
// A deployment that has relaxed something should say so on every boot. Naming
// only the protection would leave the reader hunting for which value did it,
// which is the position anybody debugging TC_ENV used to be in.
func (c *Config) logRelaxations() {
	if c.S3Endpoint != "" && c.AllowDefaultObjectStorageCredentials &&
		(c.S3AccessKey == "minioadmin" || c.S3SecretKey == "minioadmin") {
		slog.Warn("object storage is running on published default credentials",
			"allowed_by", "TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS")
	}
	if c.SMTPHost == "" && c.AllowConsoleMailer {
		slog.Warn("no mail relay configured: mail is written to the log rather than delivered, password-reset links included",
			"allowed_by", "TC_ALLOW_CONSOLE_MAILER")
	}
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

// TrustedProxyList parses TrustedProxies into netip.Prefix entries. A bare IP
// (no "/bits" suffix) is treated as an exact match. Malformed entries are
// skipped rather than failing startup, since a typo here should degrade to
// "trust nothing" rather than crash the process.
func (c *Config) TrustedProxyList() []netip.Prefix {
	var out []netip.Prefix
	for _, raw := range strings.Split(c.TrustedProxies, ",") {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			addr, err := netip.ParseAddr(s)
			if err != nil {
				continue
			}
			s = addr.String() + "/" + strconv.Itoa(addr.BitLen())
		}
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			continue
		}
		out = append(out, prefix)
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
