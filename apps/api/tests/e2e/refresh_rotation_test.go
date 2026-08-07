package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
	"github.com/stretchr/testify/require"
)

// postRefresh exchanges a refresh token without going through the helpers,
// which report failures with t.Fatalf and so cannot be called from the
// goroutines the concurrency test needs.
func postRefresh(url, refreshToken string) (int, []byte, error) {
	body, err := json.Marshal(map[string]any{"refreshToken": refreshToken})
	if err != nil {
		return 0, nil, err
	}
	resp, err := http.Post(url+"/auth/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// TestConcurrentRefreshLetsExactlyOneThrough covers the window between reading
// a session by its refresh hash and writing the next one back. Two requests
// carrying the same token used to both find the live row and both rotate it,
// so both were handed credentials while only the second one's refresh token
// survived in the table -- the first caller's was overwritten by a request it
// never made, and nothing recorded that two parties had presented the same
// token.
func TestConcurrentRefreshLetsExactlyOneThrough(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	_, refresh := signIn(t, owner)

	const attempts = 8
	statuses := make([]int, attempts)
	bodies := make([][]byte, attempts)
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statuses[i], bodies[i], errs[i] = postRefresh(testServerURL, refresh)
		}()
	}
	close(start)
	wg.Wait()

	accepted := 0
	var issued string
	for i, status := range statuses {
		require.NoError(t, errs[i])
		if status == http.StatusOK {
			accepted++
			var creds struct {
				Token string `json:"token"`
			}
			require.NoError(t, json.Unmarshal(bodies[i], &creds))
			issued = creds.Token
		} else {
			require.Equal(t, http.StatusUnauthorized, status,
				"a refresh either succeeds or is refused; nothing else is a defined answer")
		}
	}
	require.Equal(t, 1, accepted,
		"one refresh token buys one exchange, however many requests present it at once")

	// And the winner keeps what it was given. A browser resuming with several
	// expired requests sends exactly this burst, so if the losers ended the
	// session, waking a sleeping tab would sign the reader out -- the loser's
	// refusal must not reach back and undo the exchange that succeeded.
	live, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", issued, nil)
	require.Equal(t, http.StatusOK, live,
		"the exchange that won must survive the ones that lost")
}

// refreshHash is how the sessions table names a refresh token: the server
// stores only this, so a database read yields nothing usable.
func refreshHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ageOutTheExchange backdates when a session last traded its refresh token,
// so a replay arrives after the window that treats a repeat as one client
// asking twice. Waiting it out in real time would put ten seconds into every
// run of this file.
func ageOutTheExchange(t *testing.T, spent string) {
	t.Helper()
	res, err := testDB.Exec(
		`UPDATE sessions SET rotated_at = NOW(3) - INTERVAL 1 HOUR WHERE prev_refresh_hash = ?`,
		refreshHash(spent))
	require.NoError(t, err)
	rows, err := res.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), rows, "the exchange under test should have left exactly one row")
}

// TestReplayedRefreshTokenEndsTheSession covers what a spent token means. The
// rotation used to leave nothing behind that could recognise one, so a leaked
// token was quietly consumed by whoever reached the server first and the
// person it belonged to carried on with a session someone else had also held.
func TestReplayedRefreshTokenEndsTheSession(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	_, refresh := signIn(t, owner)

	var renewed struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh}, &renewed)

	live, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", renewed.Token, nil)
	require.Equal(t, http.StatusOK, live)

	// Long enough after the exchange that this cannot be the same client
	// asking twice -- which is the only reading under which continuing would
	// be safe.
	ageOutTheExchange(t, refresh)

	// The spent token comes back. Only one of the two holders can be the
	// person this session belongs to, and the server cannot tell which.
	replay, _, err := postRefresh(testServerURL, refresh)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, replay)

	after, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", renewed.Token, nil)
	require.Equal(t, http.StatusUnauthorized, after,
		"the access token issued by the exchange must stop with the session")

	spent, _, err := postRefresh(testServerURL, renewed.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, spent,
		"and so must the refresh token, or the replay only cost the wrong party")

	// The account's other sign-ins are untouched: a replay ends the session it
	// was issued for, not every device the person owns.
	other, _ := signIn(t, owner)
	elsewhere, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", other, nil)
	require.Equal(t, http.StatusOK, elsewhere)
}

// TestRepeatedRefreshRightAfterOneAnotherKeepsTheSession is the other half of
// the rule above. A browser resuming with several expired requests sends the
// same token twice within a moment, and reading the second one as a leak
// would sign the reader out for having a slow network -- a certain cost paid
// constantly, against a thief who would have to land inside the same few
// seconds.
//
// The repeat is still refused. What is being pinned is that a refusal does
// not also destroy the session.
func TestRepeatedRefreshRightAfterOneAnotherKeepsTheSession(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	_, refresh := signIn(t, owner)

	var renewed struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refreshToken"`
	}
	helpers.DoJSON(t, http.MethodPost, testServerURL+"/auth/refresh", "",
		map[string]any{"refreshToken": refresh}, &renewed)

	repeat, _, err := postRefresh(testServerURL, refresh)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, repeat, "one token still buys one exchange")

	after, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", renewed.Token, nil)
	require.Equal(t, http.StatusOK, after,
		"a repeat moments later must not end the session the first one opened")

	// And the credentials the winning exchange handed out still work.
	again, _, err := postRefresh(testServerURL, renewed.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, again)
}

// TestRefreshTokenNobodyIssuedIsJustRefused separates the replay path from an
// ordinary bad value: a string no session ever handed out must not reach the
// revocation branch, or a stranger could end sessions by guessing.
func TestRefreshTokenNobodyIssuedIsJustRefused(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	owner := helpers.NewTenant(t, testServerURL)
	access, _ := signIn(t, owner)

	status, _, err := postRefresh(testServerURL, "not-a-token-anyone-was-given")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, status)

	stillGood, _ := helpers.DoJSONStatus(t, http.MethodGet, testServerURL+"/user", access, nil)
	require.Equal(t, http.StatusOK, stillGood)
}
