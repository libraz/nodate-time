package users

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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
	oauthStateTTL   = 10 * time.Minute
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
	// AllowedDomains restricts which email domains may sign in via Google.
	// Empty means unrestricted. See config.GoogleAllowedDomainList.
	AllowedDomains []string
	// PasswordLoginEnabled reflects whether email+password auth is available,
	// surfaced to the login screen so it can hide the password form.
	PasswordLoginEnabled bool
}

// resolveProvider returns the merged provider configuration: DB row overrides
// the static env-based defaults. Returns ok=false if the provider is unknown
// or has no client_id available from any source.
func resolveProvider(ctx context.Context, deps OAuthDeps, name string) (OAuthProviderConfig, bool) {
	envCfg, _ := providerConfig(deps.Config, name)
	row, err := deps.Queries.GetOAuthProviderConfig(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		// No DB override configured; fall back to env-based defaults.
		return envCfg, envCfg.ClientID != ""
	}
	if err != nil {
		// A real DB error must fail closed — never silently fall back to env
		// credentials for a provider an admin may have intentionally disabled.
		return OAuthProviderConfig{}, false
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

const oauthStateCookieName = "oauth_state"

// oauthStateCookie builds the Set-Cookie value that binds an OAuth flow to the
// browser that started it. SameSite=Lax keeps it on the top-level redirect back
// from the provider; HttpOnly hides it from script. A negative maxAge clears it.
func oauthStateCookie(state string, secure bool, maxAge int) string {
	c := &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     "/auth/oauth",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return c.String()
}

// secureCookies reports whether OAuth cookies should carry the Secure attribute,
// inferred from whether the public redirect base is served over HTTPS.
func secureCookies(deps OAuthDeps) bool {
	return strings.HasPrefix(strings.ToLower(deps.Config.RedirectBase), "https://")
}

// idTokenNonce extracts the nonce claim from a JWT id_token without verifying
// its signature: the token was just received over TLS directly from the
// provider's token endpoint, so only the nonce binding needs checking here.
func idTokenNonce(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Nonce
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

// ListEnabledProviders reports which OAuth providers are usable (have a client
// ID from env or DB and are not disabled). Public, so the login screen can show
// only the buttons that will actually work. No secrets are exposed.
func ListEnabledProviders(deps OAuthDeps) func(context.Context, *OAuthProvidersInput) (*OAuthProvidersOutput, error) {
	return func(ctx context.Context, _ *OAuthProvidersInput) (*OAuthProvidersOutput, error) {
		out := &OAuthProvidersOutput{}
		out.Body.Providers = make([]string, 0, 2)
		out.Body.PasswordEnabled = deps.PasswordLoginEnabled
		for _, p := range []string{"google", "line"} {
			if _, ok := resolveProvider(ctx, deps, p); ok {
				out.Body.Providers = append(out.Body.Providers, p)
			}
		}
		return out, nil
	}
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

		// PKCE (RFC 7636): bind the authorization code to a one-time verifier so a
		// stolen/injected code cannot be exchanged without it.
		verifier, challenge, err := auth.GeneratePKCE()
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		// OIDC nonce: bind the returned id_token to this request (LINE).
		nonce := ""
		if in.Provider == "line" {
			nonce, err = auth.RandomHex(16)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
		}

		if err := deps.Queries.CreateOAuthState(ctx, generated.CreateOAuthStateParams{
			StateHash:    hashState(state),
			Provider:     generated.OauthStatesProvider(in.Provider),
			Redirect:     safeRedirect(in.Redirect),
			CodeVerifier: verifier,
			Nonce:        nonce,
			ExpiresAt:    time.Now().Add(oauthStateTTL),
		}); err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		params := url.Values{}
		params.Set("response_type", "code")
		params.Set("client_id", pc.ClientID)
		params.Set("redirect_uri", redirectURI(deps.Config, in.Provider))
		params.Set("scope", pc.Scopes)
		params.Set("state", state)
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")
		if nonce != "" {
			params.Set("nonce", nonce)
		}

		out := &OAuthStartOutput{
			Status:    http.StatusFound,
			URL:       pc.AuthURL + "?" + params.Encode(),
			SetCookie: oauthStateCookie(state, secureCookies(deps), int(oauthStateTTL.Seconds())),
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
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Hd            string `json:"hd"`
}

type lineUserinfo struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func exchangeCode(ctx context.Context, pc OAuthProviderConfig, code, redirect, codeVerifier string) (accessToken, idToken string, err error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("client_id", pc.ClientID)
	form.Set("client_secret", pc.ClientSecret)
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("oauth token exchange failed: %s: %s", resp.Status, body)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", "", err
	}
	return tr.AccessToken, tr.IDToken, nil
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

type consumedState struct {
	Redirect     string
	CodeVerifier string
	Nonce        string
}

func consumeState(ctx context.Context, q *generated.Queries, state, provider string) (consumedState, error) {
	hash := hashState(state)
	row, err := q.ConsumeOAuthState(ctx, hash)
	if err != nil {
		return consumedState{}, err
	}
	// Atomically claim the state by deleting it: exactly one concurrent caller
	// observes RowsAffected == 1, so a replayed or duplicated callback cannot
	// consume the same state twice (CSRF replay window).
	res, derr := q.DeleteOAuthState(ctx, hash)
	if derr != nil {
		return consumedState{}, derr
	}
	if n, aerr := res.RowsAffected(); aerr != nil || n != 1 {
		return consumedState{}, errors.New("oauth: state already consumed")
	}
	if string(row.Provider) != provider || time.Now().After(row.ExpiresAt) {
		return consumedState{}, errors.New("oauth: state mismatch or expired")
	}
	return consumedState{
		Redirect:     safeRedirect(row.Redirect),
		CodeVerifier: row.CodeVerifier,
		Nonce:        row.Nonce,
	}, nil
}

// oauthDenied redirects back to the login page with an error code so the user
// sees a friendly message instead of a raw API error. Used when a Google
// account is not permitted to sign in (domain not allowed / email unverified).
func oauthDenied(deps OAuthDeps) *OAuthCallbackOutput {
	dest := strings.TrimRight(deps.WebURL, "/") + "/login?error=oauth_not_allowed"
	return &OAuthCallbackOutput{
		Status:    http.StatusFound,
		URL:       dest,
		SetCookie: oauthStateCookie("", secureCookies(deps), -1),
	}
}

func OAuthCallback(deps OAuthDeps) func(context.Context, *OAuthCallbackInput) (*OAuthCallbackOutput, error) {
	return func(ctx context.Context, in *OAuthCallbackInput) (*OAuthCallbackOutput, error) {
		// Bind the callback to the browser that started the flow: the state cookie
		// (set at OAuthStart) must be present and match the state query param.
		// This defeats login CSRF where an attacker feeds a victim their own code.
		if in.StateCookie == "" || in.StateCookie != in.State {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		st, err := consumeState(ctx, deps.Queries, in.State, in.Provider)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}
		redirectPath := st.Redirect
		pc, ok := resolveProvider(ctx, deps, in.Provider)
		if !ok {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		accessToken, idToken, err := exchangeCode(ctx, pc, in.Code, redirectURI(deps.Config, in.Provider), st.CodeVerifier)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		// Verify the OIDC nonce echoes back in the id_token, rejecting a token
		// that was minted for a different authorization request (replay).
		if st.Nonce != "" && idTokenNonce(idToken) != st.Nonce {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}

		var subject, email, name string
		// emailVerified gates automatic account linking by email: only a provider
		// that proves the user owns the address may link to an existing account.
		emailVerified := false
		switch in.Provider {
		case "google":
			var u googleUserinfo
			if err := fetchUserinfo(ctx, pc, accessToken, &u); err != nil {
				return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
			}
			// OIDC: only trust a verified email for access decisions.
			if u.Email == "" || !u.EmailVerified {
				return oauthDenied(deps), nil
			}
			allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, u.Email)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if !allowed {
				return oauthDenied(deps), nil
			}
			subject, email, name = u.Sub, u.Email, u.Name
			emailVerified = true
		case "line":
			var u lineUserinfo
			if err := fetchUserinfo(ctx, pc, accessToken, &u); err != nil {
				return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
			}
			// The allow-list applies to every provider. LINE does not return a
			// verified-email proof, so when a domain/email restriction is active
			// and LINE gives no allow-listed address, sign-in is denied.
			allowed, err := emailAllowedToSignIn(ctx, deps.Queries, deps.AllowedDomains, u.Email)
			if err != nil {
				return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
			}
			if !allowed {
				return oauthDenied(deps), nil
			}
			// LINE's email claim is not a verified ownership proof: never use it
			// to auto-link to a pre-existing account.
			subject, email, name = u.Sub, u.Email, u.Name
			emailVerified = false
		}
		if subject == "" {
			return nil, apierrors.ToHuma(apierrors.AuthOAuthFailed)
		}
		if name == "" {
			name = "OAuth User"
		}
		email = strings.ToLower(strings.TrimSpace(email))

		userID, err := upsertOAuthUser(ctx, deps.DB, in.Provider, subject, email, name, emailVerified)
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
		return &OAuthCallbackOutput{
			Status:    http.StatusFound,
			URL:       dest,
			SetCookie: oauthStateCookie("", secureCookies(deps), -1),
		}, nil
	}
}

// upsertOAuthUser links an OAuth identity to a user, creating one if needed.
// Wrapped in a transaction so concurrent callbacks for the same subject cannot
// produce duplicate users or orphan oauth_account rows.
func upsertOAuthUser(ctx context.Context, db *sql.DB, provider, subject, email, name string, emailVerified bool) (uint32, error) {
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

	// Auto-link to an existing account by email only when the provider has
	// verified the user owns that address. Without this, an attacker who sets a
	// victim's email on an unverified provider profile (e.g. LINE) could take
	// over the victim's account.
	if emailVerified && email != "" {
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
	// When the email is missing or unverified, mint a synthetic, namespaced
	// address for the users row so it can never collide with — or be mistaken
	// for — a real, verified account.
	userEmail := email
	if !emailVerified || userEmail == "" {
		userEmail = subject + "@oauth." + provider + ".local"
	}
	accountEmail := email
	if accountEmail == "" {
		accountEmail = userEmail
	}
	res, err := q.CreateUser(ctx, generated.CreateUserParams{
		PublicID:     pubID[:],
		Name:         name,
		Email:        userEmail,
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
		Email:           accountEmail,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uid, nil
}
