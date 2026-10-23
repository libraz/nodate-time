package e2e

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// cspDirective returns the sources one directive of a policy lists.
func cspDirective(policy, name string) []string {
	for _, d := range strings.Split(policy, ";") {
		fields := strings.Fields(d)
		if len(fields) > 0 && fields[0] == name {
			return fields[1:]
		}
	}
	return nil
}

// The /docs page loads its renderer from a CDN. Under the API's JSON policy of
// default-src 'none' the browser blocks both files and the page is blank, so
// the page needs a policy of its own -- one that names exactly the files the
// page references, and is checked against the page itself so a version bump
// on one side cannot leave the other behind.
func TestDocsPagePolicyAdmitsWhatThePageLoads(t *testing.T) {
	bootstrap(t)
	t.Parallel()

	resp, err := http.Get(testServerURL + "/docs")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	policy := resp.Header.Get("Content-Security-Policy")

	scripts := regexp.MustCompile(`<script[^>]*\ssrc="([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	styles := regexp.MustCompile(`<link[^>]*rel="stylesheet"[^>]*href="([^"]+)"`).FindAllStringSubmatch(string(body), -1)
	require.NotEmpty(t, scripts, "the page should load its renderer")
	require.NotEmpty(t, styles, "the page should load its stylesheet")
	for _, m := range scripts {
		require.Contains(t, cspDirective(policy, "script-src"), m[1], "policy: %s", policy)
	}
	for _, m := range styles {
		require.Contains(t, cspDirective(policy, "style-src"), m[1], "policy: %s", policy)
	}
	require.Contains(t, cspDirective(policy, "connect-src"), "'self'",
		"the page fetches the schema from this origin")
	require.Contains(t, cspDirective(policy, "frame-ancestors"), "'none'")

	jsonResp, err := http.Get(testServerURL + "/openapi.json")
	require.NoError(t, err)
	defer jsonResp.Body.Close()
	jsonPolicy := jsonResp.Header.Get("Content-Security-Policy")
	require.Equal(t, []string{"'none'"}, cspDirective(jsonPolicy, "default-src"))
	require.Nil(t, cspDirective(jsonPolicy, "script-src"), "a JSON response must not admit scripts: %s", jsonPolicy)
}
