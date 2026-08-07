package e2e

import (
	"bytes"
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
	errs := make([]error, attempts)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statuses[i], _, errs[i] = postRefresh(testServerURL, refresh)
		}()
	}
	close(start)
	wg.Wait()

	accepted := 0
	for i, status := range statuses {
		require.NoError(t, errs[i])
		if status == http.StatusOK {
			accepted++
		} else {
			require.Equal(t, http.StatusUnauthorized, status,
				"a refresh either succeeds or is refused; nothing else is a defined answer")
		}
	}
	require.Equal(t, 1, accepted,
		"one refresh token buys one exchange, however many requests present it at once")
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
