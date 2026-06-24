package config

import "testing"

// baseProdConfig returns a config that passes production validation, so each
// test can flip a single field to assert that guard.
func baseProdConfig() *Config {
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

func TestValidateProductionGuards(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{"valid prod config", func(*Config) {}, false},
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
			c := baseProdConfig()
			tt.mutate(c)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDevModeSkipsGuards(t *testing.T) {
	// In development, insecure defaults are tolerated for convenience.
	c := &Config{Env: "development", JWTSecret: defaultJWTSecret, S3AccessKey: "minioadmin", S3SecretKey: "minioadmin"}
	if err := c.Validate(); err != nil {
		t.Fatalf("dev config should pass validation, got %v", err)
	}
}
