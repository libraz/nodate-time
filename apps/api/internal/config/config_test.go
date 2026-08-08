package config

import (
	"testing"

	"github.com/caarlos0/env/v11"
)

// baseConfig returns a config that passes validation, so each test can flip a
// single field to assert that guard.
func baseConfig() *Config {
	return &Config{
		Env:                "production",
		JWTSecret:          "a-sufficiently-long-production-secret-value",
		S3Endpoint:         "s3.example.com",
		S3AccessKey:        "real-access-key",
		S3SecretKey:        "real-secret-key",
		CORSAllowedOrigins: "https://app.example.com",
		SMTPHost:           "smtp.example.com",
	}
}

func TestValidateGuards(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid config", func(*Config) {}, false},
		{"default jwt secret rejected", func(c *Config) { c.JWTSecret = defaultJWTSecret }, true},
		{"empty jwt secret rejected", func(c *Config) { c.JWTSecret = "" }, true},
		{"short jwt secret rejected", func(c *Config) { c.JWTSecret = "tooshort" }, true},
		{"default s3 creds rejected", func(c *Config) { c.S3AccessKey = "minioadmin"; c.S3SecretKey = "minioadmin" }, true},
		{"cors wildcard rejected", func(c *Config) { c.CORSAllowedOrigins = "*" }, true},
		{"empty cors rejected", func(c *Config) { c.CORSAllowedOrigins = "" }, true},
		{"missing smtp host rejected", func(c *Config) { c.SMTPHost = "" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseConfig()
			tt.mutate(c)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

// TestDevelopmentDisablesNoGuard is the whole point of splitting the flag.
//
// TC_ENV used to answer all four of these at once, so a deployment that set it
// to get /auth/dev-login also stopped checking its signing secret, its object
// storage credentials, its CORS list and its mail relay -- and said nothing.
// Each guard is now answered on its own, and the environment answers none.
func TestDevelopmentDisablesNoGuard(t *testing.T) {
	guards := []struct {
		name   string
		mutate func(*Config)
	}{
		{"the published signing secret", func(c *Config) { c.JWTSecret = defaultJWTSecret }},
		{"the published object storage credentials", func(c *Config) {
			c.S3AccessKey = "minioadmin"
			c.S3SecretKey = "minioadmin"
		}},
		{"a wildcard CORS origin", func(c *Config) { c.CORSAllowedOrigins = "*" }},
		{"no mail relay", func(c *Config) { c.SMTPHost = "" }},
	}
	for _, g := range guards {
		t.Run(g.name, func(t *testing.T) {
			c := baseConfig()
			c.Env = "development"
			g.mutate(c)
			if err := c.Validate(); err == nil {
				t.Fatalf("development must not excuse %s", g.name)
			}
		})
	}
}

// TestTheTwoAllowancesAreNamedAndSeparate covers what a developer actually
// needs: the local object store ships with published credentials and there is
// no relay to send mail through. Each is asked for by name, so answering one
// cannot answer the other -- which is the failure TC_ENV had.
func TestTheTwoAllowancesAreNamedAndSeparate(t *testing.T) {
	t.Run("object storage credentials, allowed", func(t *testing.T) {
		c := baseConfig()
		c.S3AccessKey = "minioadmin"
		c.S3SecretKey = "minioadmin"
		c.AllowDefaultObjectStorageCredentials = true
		if err := c.Validate(); err != nil {
			t.Fatalf("the named allowance should permit the local object store, got %v", err)
		}
	})

	t.Run("console mailer, allowed", func(t *testing.T) {
		c := baseConfig()
		c.SMTPHost = ""
		c.AllowConsoleMailer = true
		if err := c.Validate(); err != nil {
			t.Fatalf("the named allowance should permit running without a relay, got %v", err)
		}
	})

	// Neither allowance may stand in for the other, and neither touches the
	// two guards that have no allowance at all.
	t.Run("one allowance does not answer the other", func(t *testing.T) {
		c := baseConfig()
		c.AllowConsoleMailer = true
		c.S3AccessKey = "minioadmin"
		c.S3SecretKey = "minioadmin"
		if err := c.Validate(); err == nil {
			t.Fatal("allowing the console mailer must not also allow default storage credentials")
		}
	})

	t.Run("no allowance reaches the signing secret", func(t *testing.T) {
		c := baseConfig()
		c.AllowConsoleMailer = true
		c.AllowDefaultObjectStorageCredentials = true
		c.JWTSecret = defaultJWTSecret
		if err := c.Validate(); err == nil {
			t.Fatal("the signing secret has no allowance and must always be required")
		}
	})
}

// TestDevelopmentDefaultsPassTheGuardsTheyNeverNeeded records the finding that
// shaped this change: two of the four exemptions were never protecting a
// development need.
//
// The built-in CORS list is already two explicit localhost origins, and the
// built-in signing secret already clears the length rule -- it fails only on
// being the one published here. So neither guard ever stood between a
// developer and a working local stack; the exemptions only kept a genuine
// mistake from being noticed, in the environment where noticing it is free.
func TestDevelopmentDefaultsPassTheGuardsTheyNeverNeeded(t *testing.T) {
	c := &Config{}
	if err := env.Parse(c); err != nil {
		t.Fatalf("parsing the built-in defaults: %v", err)
	}

	if len(c.CORSAllowedOriginList()) == 0 {
		t.Fatal("the built-in CORS list should be non-empty")
	}
	for _, o := range c.CORSAllowedOriginList() {
		if o == "" || o == "*" {
			t.Fatalf("the built-in CORS list should already satisfy the guard, got %q", o)
		}
	}

	if len(c.JWTSecret) < 32 {
		t.Fatalf("the built-in secret should already clear the length rule, got %d bytes", len(c.JWTSecret))
	}
	if c.JWTSecret != defaultJWTSecret {
		t.Fatal("the built-in secret should be the published default, which is the only reason it is refused")
	}
}

func TestTrustedProxyListParsesBareIPsAndCIDRsAndSkipsGarbage(t *testing.T) {
	c := &Config{TrustedProxies: " 10.0.0.1 , 192.168.1.0/24,not-an-ip, ,2001:db8::1"}
	got := c.TrustedProxyList()
	if len(got) != 3 {
		t.Fatalf("expected 3 valid entries, got %d: %v", len(got), got)
	}
	if got[0].String() != "10.0.0.1/32" {
		t.Fatalf("expected bare IPv4 to become a /32, got %s", got[0])
	}
	if got[1].String() != "192.168.1.0/24" {
		t.Fatalf("expected explicit CIDR to pass through unchanged, got %s", got[1])
	}
	if got[2].String() != "2001:db8::1/128" {
		t.Fatalf("expected bare IPv6 to become a /128, got %s", got[2])
	}
}

func TestTrustedProxyListEmptyMeansNoProxyTrusted(t *testing.T) {
	c := &Config{}
	if got := c.TrustedProxyList(); len(got) != 0 {
		t.Fatalf("expected no trusted proxies for empty config, got %v", got)
	}
}
