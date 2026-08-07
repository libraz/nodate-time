package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/app"
	"github.com/libraz/nodate-time/apps/api/internal/http/router"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// What guessing a password, flooding a mailbox and forging a client address
// actually cost an attacker.
//
// The package-wide server runs with the limiter switched off, so that parallel
// tenants can all register from one loopback address. Nothing in the suite
// exercises the limiter unless a test brings its own server, which is why
// these do.

// newLimitedServer builds a server with the auth limiter on, at a limit small
// enough to reach in a handful of requests.
func newLimitedServer(t *testing.T, limit int, trusted []netip.Prefix) string {
	t.Helper()
	queries := generated.New(testDB)
	deps := router.Deps{
		DB:                   testDB,
		Queries:              queries,
		WorkspaceID:          helpers.TestWorkspace(queries).ID,
		JWTSecret:            helpers.TestJWTSecret,
		Mailer:               testMailer,
		WebURL:               helpers.TestWebURL,
		PasswordLoginEnabled: true,
		AuthRateLimit:        limit,
		ShareRateLimit:       -1,
		TrustedProxies:       trusted,
	}
	srv := httptest.NewServer(app.NewHandler(deps, app.Options{}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// postAuth sends one request to an unauthenticated auth endpoint, optionally
// claiming a forwarded client address.
func postAuth(t *testing.T, url, path string, body any, xff string) int {
	t.Helper()
	headers := map[string]string{}
	if xff != "" {
		headers["X-Forwarded-For"] = xff
	}
	status, _ := helpers.DoJSONStatusWithHeaders(t, http.MethodPost, url+path, "", body, headers)
	return status
}

// spentToken is a refresh token no session ever issued. It reaches the handler
// and is refused there, so a 429 in its place can only have come from the
// limiter in front of it.
var spentToken = map[string]any{"refreshToken": "nobody-issued-this"}

// Refreshing is what a client does on a schedule, and the endpoint now has a
// path that ends a session when a token is presented twice. Being able to
// drive it without limit is therefore worth more than it used to be, so
// whether it sits inside the limited group is settled here rather than read
// off the route list.
func TestRefreshIsInsideTheRateLimitedGroup(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url := newLimitedServer(t, 3, nil)

	for i := range 3 {
		status, _, hdr := helpers.DoJSONFull(t, http.MethodPost, url+"/auth/refresh", "",
			spentToken, nil)
		require.Equal(t, http.StatusUnauthorized, status, "request %d", i)
		require.Equal(t, "3", hdr.Get("X-RateLimit-Limit"),
			"the limiter has to be in front of this endpoint to advertise a budget for it")
	}

	status, raw, hdr := helpers.DoJSONFull(t, http.MethodPost, url+"/auth/refresh", "", spentToken, nil)
	require.Equal(t, http.StatusTooManyRequests, status, "body: %s", string(raw))
	require.Contains(t, string(raw), "RATE.LIMITED",
		"a refusal by the limiter must arrive in the same envelope as every other error")
	require.NotEmpty(t, hdr.Get("Retry-After"))
}

// One limiter covers the whole unauthenticated auth group, so the budget is
// shared: guessing passwords spends the same allowance as registering or
// asking for a reset. Behind a proxy that reports real client addresses this
// is per attacker. Behind one that does not -- or for everyone sharing an
// office address -- it means one client's traffic can close the others out.
func TestOneBudgetCoversEveryPasswordEndpoint(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url := newLimitedServer(t, 3, nil)
	for range 3 {
		require.Equal(t, http.StatusUnauthorized, postAuth(t, url, "/auth/login",
			map[string]any{"email": "nobody@test.local", "password": "wrongpassword"}, ""))
	}

	require.Equal(t, http.StatusTooManyRequests, postAuth(t, url, "/auth/register",
		map[string]any{"name": "X", "email": "someone@test.local", "password": "password123"}, ""))
	require.Equal(t, http.StatusTooManyRequests, postAuth(t, url,
		"/auth/password-reset/request", map[string]any{"email": "someone@test.local"}, ""))
	require.Equal(t, http.StatusTooManyRequests, postAuth(t, url, "/auth/refresh", spentToken, ""))
}

// Liveness is not credential traffic. A probe that got throttled would take
// the instance out of rotation for being asked whether it was alive.
func TestHealthIsNotRateLimited(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url := newLimitedServer(t, 2, nil)
	for i := range 6 {
		resp, err := http.Get(url + "/health")
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, "probe %d", i)
	}
}

// With no proxy configured, the forwarded header is somebody's claim about
// themselves and is ignored entirely. A client that could be believed would
// have an unlimited budget by writing a new address on every request.
func TestForgedForwardedForCannotEscapeItsOwnBucket(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url := newLimitedServer(t, 3, nil)
	for i := range 3 {
		require.Equal(t, http.StatusUnauthorized,
			postAuth(t, url, "/auth/refresh", spentToken, fmt.Sprintf("203.0.113.%d", i+1)))
	}
	require.Equal(t, http.StatusTooManyRequests,
		postAuth(t, url, "/auth/refresh", spentToken, "203.0.113.99"),
		"a new address on every request must not buy a new budget")
}

// Behind a proxy that is trusted, the header is how the real client is known,
// and the rightmost hop that is not itself a proxy is the one believed. A
// client prepending entries of its own writes them to the left of that, so it
// cannot move itself out of its own bucket -- nor into someone else's by
// claiming the whole chain is proxies.
func TestForwardedForIsHonouredOnlyFromATrustedPeer(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url := newLimitedServer(t, 2, helpers.LoopbackProxies())

	require.Equal(t, http.StatusUnauthorized, postAuth(t, url, "/auth/refresh", spentToken, "203.0.113.1"))
	require.Equal(t, http.StatusUnauthorized, postAuth(t, url, "/auth/refresh", spentToken, "203.0.113.1"))
	require.Equal(t, http.StatusTooManyRequests, postAuth(t, url, "/auth/refresh", spentToken, "203.0.113.1"))

	require.Equal(t, http.StatusUnauthorized,
		postAuth(t, url, "/auth/refresh", spentToken, "203.0.113.2"),
		"a different client keeps its own budget")

	require.Equal(t, http.StatusTooManyRequests,
		postAuth(t, url, "/auth/refresh", spentToken, "1.1.1.1, 203.0.113.1"),
		"an entry prepended by the client must not shift it out of its own bucket")

	require.Equal(t, http.StatusUnauthorized,
		postAuth(t, url, "/auth/refresh", spentToken, "127.0.0.1, ::1"),
		"a chain claiming to be nothing but proxies falls back to the peer's own bucket")
}

// registerFor creates an account on the package server and returns its access
// token, for the login tests below.
func registerFor(t *testing.T, email, password string) string {
	t.Helper()
	var reg struct {
		Token string `json:"token"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/register", "",
		map[string]any{"name": "Lockout", "email": email, "password": password}, &reg)
	return reg.Token
}

func loginStatus(t *testing.T, email, password string) (int, []byte) {
	t.Helper()
	return helpers.DoJSONStatus(t, http.MethodPost, testServerURL+"/auth/login", "",
		map[string]any{"email": email, "password": password})
}

// identityLockState reads the counter and the lock straight from the row,
// which is the only place either is visible.
func identityLockState(t *testing.T, email string) (int, sql.NullTime) {
	t.Helper()
	var attempts int
	var locked sql.NullTime
	err := testDB.QueryRow(
		`SELECT failed_attempts, locked_until_at FROM identities WHERE provider='local' AND subject=?`,
		email).Scan(&attempts, &locked)
	require.NoError(t, err)
	return attempts, locked
}

// expireLock moves the lock into the past without touching the counter, which
// is what the passage of fifteen minutes does. The alternative is a test that
// sleeps for a quarter of an hour.
func expireLock(t *testing.T, email string) {
	t.Helper()
	_, err := testDB.Exec(
		`UPDATE identities SET locked_until_at = NOW(3) - INTERVAL 1 MINUTE
		 WHERE provider='local' AND subject=?`, email)
	require.NoError(t, err)
}

func TestLoginLocksAnIdentityAfterTenWrongPasswords(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	email := fmt.Sprintf("lock-threshold-%d@test.local", time.Now().UnixNano())
	const password = "correcthorsebattery"
	registerFor(t, email, password)

	for i := range 9 {
		status, _ := loginStatus(t, email, "definitely-not-it")
		require.Equal(t, http.StatusUnauthorized, status, "guess %d", i+1)
		_, locked := identityLockState(t, email)
		require.False(t, locked.Valid, "still unlocked after %d guesses", i+1)
	}

	status, _ := loginStatus(t, email, "definitely-not-it")
	require.Equal(t, http.StatusUnauthorized, status)
	attempts, locked := identityLockState(t, email)
	require.Equal(t, 10, attempts)
	require.True(t, locked.Valid, "the tenth wrong password locks the identity")

	status, _ = loginStatus(t, email, password)
	require.Equal(t, http.StatusUnauthorized, status,
		"the owner's own password is refused while the lock stands")
}

// countingMailer records what was sent, so a test can ask how much mail one
// address was made to receive.
type countingMailer struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (m *countingMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *countingMailer) countFor(to string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, msg := range m.sent {
		if msg.To == to {
			n++
		}
	}
	return n
}

// mailServer boots a server whose mail can be counted. The auth limiter is off
// on it, so what these tests measure is the send budget itself rather than the
// request limiter in front of it.
func mailServer(t *testing.T) (string, *countingMailer) {
	t.Helper()
	mc := &countingMailer{}
	return helpers.NewTestServerWithMailer(t, testDB, mc).BaseURL, mc
}

func requestReset(t *testing.T, url, email, xff string) {
	t.Helper()
	status := postAuth(t, url, "/auth/password-reset/request", map[string]any{"email": email}, xff)
	require.Equal(t, http.StatusOK, status,
		"the answer is the same whether or not mail was sent, so it cannot be used to probe")
}

func TestPasswordResetMailIsCappedPerAddressAndClient(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url, mc := mailServer(t)
	email := fmt.Sprintf("flood-cap-%d@test.local", time.Now().UnixNano())
	helpers.DoJSON(t, http.MethodPost, url+"/auth/register", "",
		map[string]any{"name": "Flood", "email": email, "password": "correcthorsebattery"}, nil)
	registration := mc.countFor(email)
	require.Equal(t, 1, registration, "the confirmation the registration sends")

	for range 5 {
		requestReset(t, url, email, "198.51.100.1")
	}
	require.Equal(t, registration+3, mc.countFor(email),
		"one client buys three reset mails an hour for one address, and no more")
}

// The half of that scoping which does work: an attacker hammering a victim's
// address from their own machine spends their own allowance, not the victim's.
func TestOneClientCannotSpendAnotherClientsResetBudget(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url, mc := mailServer(t)
	email := fmt.Sprintf("flood-scope-%d@test.local", time.Now().UnixNano())
	helpers.DoJSON(t, http.MethodPost, url+"/auth/register", "",
		map[string]any{"name": "Flood", "email": email, "password": "correcthorsebattery"}, nil)

	for range 5 {
		requestReset(t, url, email, "198.51.100.6")
	}
	before := mc.countFor(email)

	requestReset(t, url, email, "198.51.100.7")
	require.Equal(t, before+1, mc.countFor(email),
		"the owner can still ask for their own reset after someone else has exhausted theirs")
}

// Asking about an address nobody registered sends nothing and answers exactly
// as it does for one that exists.
func TestPasswordResetSaysNothingAboutWhetherAnAddressExists(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	url, mc := mailServer(t)
	ghost := fmt.Sprintf("ghost-%d@test.local", time.Now().UnixNano())
	requestReset(t, url, ghost, "198.51.100.8")
	require.Zero(t, mc.countFor(ghost))
}
