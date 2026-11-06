package config

import (
	"strings"
	"testing"
)

func TestResolveImageURL(t *testing.T) {
	const png = "data:image/png;base64,"
	const shapeErr = "must be an https URL or a data:image/(png|svg+xml|webp|jpeg|gif) URI"
	const asciiErr = "must contain only printable ASCII characters"

	accepted := []struct{ label, in, want string }{
		{"empty", "", ""},
		{"spaces", "   ", ""},
		{"tab and newline", "\t\n", ""},
		{"trimmed", " https://cdn.example/l.svg ", "https://cdn.example/l.svg"},
		{"upper https", "HTTPS://cdn.example/l.svg", "HTTPS://cdn.example/l.svg"},
		{"upper data", "DATA:image/png;base64,AAAA", "DATA:image/png;base64,AAAA"},
		{"svg data", "data:image/svg+xml,%3Csvg%3E", "data:image/svg+xml,%3Csvg%3E"},
		{"data at limit", png + strings.Repeat("A", 32768-len(png)), png + strings.Repeat("A", 32768-len(png))},
	}
	for _, c := range accepted {
		t.Run("accepts "+c.label, func(t *testing.T) {
			got, err := resolveImageURL("TC_APP_LOGO_URL", c.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}

	rejected := []struct{ label, in, msg string }{
		{"bmp", "data:image/bmp;base64,AAAA", shapeErr},
		{"http", "http://cdn.example/l.svg", shapeErr},
		{"no host", "https://", shapeErr},
		{"no scheme", "cdn.example/l.svg", shapeErr},
		{"inner space", "https://cdn.example/a b.svg", asciiErr},
		{"newline", "https://a.example/x\ny", asciiErr},
		{"non-ascii", "https://例え.jp/l.svg", asciiErr},
		{"data over limit", png + strings.Repeat("A", 32769-len(png)), "data URI must be at most 32768 characters"},
	}
	for _, c := range rejected {
		t.Run("rejects "+c.label, func(t *testing.T) {
			_, err := resolveImageURL("TC_APP_ICON_URL", c.in)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), "TC_APP_ICON_URL") || !strings.Contains(err.Error(), c.msg) {
				t.Errorf("error = %q, want it to name the variable and contain %q", err, c.msg)
			}
		})
	}
}

func TestLoadValidatesBrandImages(t *testing.T) {
	t.Setenv("TC_JWT_SECRET", "a-sufficiently-long-production-secret-value")
	t.Setenv("TC_CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS", "true")

	t.Setenv("TC_APP_LOGO_URL", "  https://cdn.example/l.svg ")
	t.Setenv("TC_APP_ICON_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppLogoURL != "https://cdn.example/l.svg" || cfg.AppIconURL != "" {
		t.Errorf("got logo %q icon %q", cfg.AppLogoURL, cfg.AppIconURL)
	}

	t.Setenv("TC_APP_ICON_URL", "http://cdn.example/i.svg")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TC_APP_ICON_URL") {
		t.Errorf("Load error = %v, want one naming TC_APP_ICON_URL", err)
	}
}
