package users

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/auth"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
)

const (
	oauthStateTTL  = 10 * time.Minute
	oauthStateBytes = 24
)

type OAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserinfoURL  string
	Scopes       string
}

type OAuthConfig struct {
	RedirectBase string
	Google       OAuthProviderConfig
	LINE         OAuthProviderConfig
}

type OAuthDeps struct {
	DB        *sql.DB
	Queries   *generated.Queries
	JWTSecret string
	WebURL    string
	Config    OAuthConfig
	Cipher    *secrets.Cipher
}

// resolveProvider returns the merged provider configuration: DB row overrides
// the static env-based defaults. Returns ok=false if the provider is unknown
// or has no client_id available from any source.
func resolveProvider(ctx context.Context, deps OAuthDeps, name string) (OAuthProviderConfig, bool) {
	envCfg, _ := providerConfig(deps.Config, name)
	row, err := deps.Queries.GetOAuthProviderConfig(ctx, name)
	if err != nil {
		return envCfg, envCfg.ClientID != ""
	}
	if !row.Enabled {
		return OAuthProviderConfig{}, false
	}
	merged := envCfg
	if row.ClientID != "" {
		merged.ClientID = row.ClientID
	}
	if len(row.ClientSecretEnc) > 0 && deps.Cipher.Available() {
		if plain, err := deps.Cipher.Decrypt(row.ClientSecretEnc); err == nil {
			merged.ClientSecret = string(plain)
		}
	}
	return merged, merged.ClientID != ""
}

func providerConfig(cfg OAuthConfig, provider string) (OAuthProviderConfig, bool) {
	switch provider {
	case "google":
		return cfg.Google, cfg.Google.ClientID != ""
	case "line":
		return cfg.LINE, cfg.LINE.ClientID != ""
	}
	return OAuthProviderConfig{}, false
}

func redirectURI(cfg OAuthConfig, provider string) string {
	return strings.TrimRight(cfg.RedirectBase, "/") + "/auth/oauth/" + provider + "/callback"
}

func hashState(state string) string {
	h := sha256.Sum256([]byte(state))
	return hex.EncodeToString(h[:])
}

// safeRedirect returns a path safe to redirect the user to after OAuth.
// Only same-origin paths starting with "/" (and not "//") are accepted to avoid open redirect.
func safeRedirect(raw string) string {
	if raw == "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	if strings.ContainsAny(raw, "\r\n\\") {
		return "/"
	}
	return raw
}

func OAuthStart(deps OAuthDeps) func(context.Context, *OAuthStartInput) (*OAuthStartOutput, error) {
	return func(ctx context.Context, in *OAuthStartInput) (*OAuthStartOutput, error) {
		pc, ok := resolveProvider(ctx, deps, in.Provider)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		state, err := auth.RandomHex(oauthStateBytes)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := deps.Queries.CreateOAuthState(ctx, generated.CreateOAuthStateParams{
			StateHash: hashState(state),
			Provider:  generated.OauthStatesProvider(in.Provider),
			Redirect:  safeRedirect(in.Redirect),
			ExpiresAt: time.Now().Add(oauthStateTTL),
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		params := url.Values{}
		params.Set("response_type", "code")
		params.Set("client_id", pc.ClientID)
		params.Set("redirect_uri", redirectURI(deps.Config, in.Provider))
		params.Set("scope", pc.Scopes)
		params.Set("state", state)
		if in.Provider == "line" {
			nonce, err := auth.RandomHex(16)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			params.Set("nonce", nonce)
		}

		out := &OAuthStartOutput{
			Status: http.StatusFound,
			URL:    pc.AuthURL + "?" + params.Encode(),
		}
		out.Body.AuthorizeURL = out.URL
		out.Body.State = state
		return out, nil
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
}

type googleUserinfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type lineUserinfo struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func exchangeCode(ctx context.Context, pc OAuthProviderConfig, code, redirect string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("client_id", pc.ClientID)
	form.Set("client_secret", pc.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("oauth token exchange failed: %s: %s", resp.Status, body)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	return tr.AccessToken, nil
}

func fetchUserinfo(ctx context.Context, pc OAuthProviderConfig, accessToken string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pc.UserinfoURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("oauth userinfo failed: %s: %s", resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func consumeState(ctx context.Context, q *generated.Queries, state, provider string) (string, error) {
	hash := hashState(state)
	row, err := q.ConsumeOAuthState(ctx, hash)
	if err != nil {
		return "", err
	}
	// Always delete (best-effort) so a state cannot be reused.
	_ = q.DeleteOAuthState(ctx, hash)
	if string(row.Provider) != provider || time.Now().After(row.ExpiresAt) {
		return "", errors.New("oauth: state mismatch or expired")
	}
	return safeRedirect(row.Redirect), nil
}

func OAuthCallback(deps OAuthDeps) func(context.Context, *OAuthCallbackInput) (*OAuthCallbackOutput, error) {
	return func(ctx context.Context, in *OAuthCallbackInput) (*OAuthCallbackOutput, error) {
		redirectPath, err := consumeState(ctx, deps.Queries, in.State, in.Provider)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}
		pc, ok := resolveProvider(ctx, deps, in.Provider)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		accessToken, err := exchangeCode(ctx, pc, in.Code, redirectURI(deps.Config, in.Provider))
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		var subject, email, name string
		switch in.Provider {
		case "google":
			var u googleUserinfo
			if err := fetchUserinfo(ctx, pc, accessToken, &u); err != nil {
				return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
			}
			subject, email, name = u.Sub, u.Email, u.Name
		case "line":
			var u lineUserinfo
			if err := fetchUserinfo(ctx, pc, accessToken, &u); err != nil {
				return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
			}
			subject, email, name = u.Sub, u.Email, u.Name
		}
		if subject == "" {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}
		if name == "" {
			name = "OAuth User"
		}

		userID, err := upsertOAuthUser(ctx, deps.DB, in.Provider, subject, email, name)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		token, err := auth.GenerateToken(userID, deps.JWTSecret)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// Token is delivered via URL fragment (#token=...) so it is not sent
		// to the server, recorded in access logs, or leaked via Referer header.
		dest := strings.TrimRight(deps.WebURL, "/") + "/oauth-complete?redirect=" +
			url.QueryEscape(redirectPath) + "#token=" + url.QueryEscape(token)
		return &OAuthCallbackOutput{Status: http.StatusFound, URL: dest}, nil
	}
}

// upsertOAuthUser links an OAuth identity to a user, creating one if needed.
// Wrapped in a transaction so concurrent callbacks for the same subject cannot
// produce duplicate users or orphan oauth_account rows.
func upsertOAuthUser(ctx context.Context, db *sql.DB, provider, subject, email, name string) (uint32, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	q := generated.New(tx)

	if existing, err := q.GetOAuthAccount(ctx, generated.GetOAuthAccountParams{
		Provider:        generated.OauthAccountsProvider(provider),
		ProviderSubject: subject,
	}); err == nil {
		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return existing.UserID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	if email != "" {
		if u, err := q.GetUserByEmail(ctx, email); err == nil {
			if _, err := q.CreateOAuthAccount(ctx, generated.CreateOAuthAccountParams{
				UserID:          u.ID,
				Provider:        generated.OauthAccountsProvider(provider),
				ProviderSubject: subject,
				Email:           email,
			}); err != nil {
				return 0, err
			}
			if err := tx.Commit(); err != nil {
				return 0, err
			}
			return u.ID, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}

	pubID, err := uuid.NewV7()
	if err != nil {
		return 0, err
	}
	if email == "" {
		email = subject + "@oauth." + provider + ".local"
	}
	res, err := q.CreateUser(ctx, generated.CreateUserParams{
		PublicID:     pubID[:],
		Name:         name,
		Email:        email,
		Icon:         "👤",
		Color:        "#42A5F5",
		PasswordHash: "!", // placeholder — user has no password, must use OAuth
	})
	if err != nil {
		return 0, err
	}
	insertID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	uid := uint32(insertID)

	if _, err := q.CreateOAuthAccount(ctx, generated.CreateOAuthAccountParams{
		UserID:          uid,
		Provider:        generated.OauthAccountsProvider(provider),
		ProviderSubject: subject,
		Email:           email,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uid, nil
}
