package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prefixes is the shorthand the tables below are written in.
func prefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		require.NoError(t, err)
		out = append(out, p)
	}
	return out
}

// The peer kinds and allow-lists are enumerated in full rather than sampled:
// there are only eighteen pairs, every one of them is a decision about who may
// read the process's internals, and the interesting failures live in the pairs
// nobody would think to write down -- an IPv4-mapped peer against an IPv4
// prefix, a public address against a list that never mentions it.
func TestPeerAllowedAcrossEveryPeerAndAllowList(t *testing.T) {
	t.Parallel()

	defaults := []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

	allowLists := []struct {
		name    string
		entries []string
	}{
		{"default", defaults},
		{"loopback-only", []string{"127.0.0.0/8", "::1/128"}},
		{"empty", nil},
	}

	peers := []struct {
		name string
		addr string
		// allowedBy names the lists that must admit this peer. Any list not
		// named here must refuse it.
		allowedBy map[string]bool
	}{
		{"loopback-v4", "127.0.0.1:51000", map[string]bool{"default": true, "loopback-only": true}},
		{"loopback-v6", "[::1]:51000", map[string]bool{"default": true, "loopback-only": true}},
		// How a connection to 127.0.0.1 arrives at a dual-stack listener.
		{"mapped-v4-loopback", "[::ffff:127.0.0.1]:51000", map[string]bool{"default": true, "loopback-only": true}},
		{"private-v4", "172.18.0.1:51000", map[string]bool{"default": true}},
		{"public-v4", "203.0.113.9:51000", map[string]bool{}},
		{"unparseable", "not-an-address", map[string]bool{}},
	}

	for _, list := range allowLists {
		for _, peer := range peers {
			t.Run(fmt.Sprintf("%s/%s", list.name, peer.name), func(t *testing.T) {
				t.Parallel()
				got := peerAllowed(peer.addr, prefixes(t, list.entries...))
				assert.Equal(t, peer.allowedBy[list.name], got,
					"peer %s against the %s allow-list", peer.addr, list.name)
			})
		}
	}
}

// Every path on this handler is gated, not just the one anybody remembers to
// check. A profile reachable because it was mounted after the guard is the
// same disclosure as an unguarded metrics endpoint, and quieter.
func TestEveryAdminPathIsRefusedFromAnUnlistedPeer(t *testing.T) {
	t.Parallel()

	handler := NewAdminHandler(AdminOptions{
		AllowedPeers: prefixes(t, "127.0.0.0/8"),
		Health:       NewReporter(0),
	})

	paths := []string{
		"/metrics",
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
		"/health/detail",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			denied := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "203.0.113.9:51000"
			handler.ServeHTTP(denied, req)
			assert.Equal(t, http.StatusNotFound, denied.Code,
				"%s answered an unlisted peer", path)

			allowed := httptest.NewRecorder()
			req = httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "127.0.0.1:51000"
			handler.ServeHTTP(allowed, req)
			assert.NotEqual(t, http.StatusNotFound, allowed.Code,
				"%s is not reachable from a listed peer either, so the test above proves nothing", path)
		})
	}
}

// The forwarded headers are the whole reason this listener checks the peer
// rather than the client: if one of them could name the client, a single
// mistaken proxy rule would publish the profiles, and it would publish them to
// everyone at once because every request would arrive from the proxy.
func TestForwardedHeadersCannotGrantAdminAccess(t *testing.T) {
	t.Parallel()

	handler := NewAdminHandler(AdminOptions{AllowedPeers: prefixes(t, "127.0.0.0/8")})

	spoofs := []map[string]string{
		{"X-Forwarded-For": "127.0.0.1"},
		{"X-Forwarded-For": "127.0.0.1, 203.0.113.9"},
		{"X-Real-Ip": "127.0.0.1"},
		{"Forwarded": "for=127.0.0.1"},
		{"X-Forwarded-For": "::1", "X-Real-Ip": "127.0.0.1"},
	}

	for i, headers := range spoofs {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.RemoteAddr = "203.0.113.9:51000"
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusNotFound, rec.Code, "headers %v were believed", headers)
		})
	}
}

// Importing net/http/pprof registers its handlers on http.DefaultServeMux as a
// side effect. Serving that mux would export whatever any dependency has put
// on it -- a set this package cannot see and does not control.
func TestAdminHandlerDoesNotServeTheDefaultServeMux(t *testing.T) {
	t.Parallel()

	http.HandleFunc("/observability-test-canary", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodGet, "/observability-test-canary", nil)
	req.RemoteAddr = "127.0.0.1:51000"
	rec := httptest.NewRecorder()
	NewAdminHandler(AdminOptions{AllowedPeers: prefixes(t, "127.0.0.0/8")}).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"a handler registered on the default mux was reachable through the admin listener")
}

func TestMetricsAreServedToAListedPeer(t *testing.T) {
	t.Parallel()

	RecordRequest(http.MethodGet, "/observability-test-route", http.StatusOK, 0.01, 128)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:51000"
	rec := httptest.NewRecorder()
	NewAdminHandler(AdminOptions{AllowedPeers: prefixes(t, "127.0.0.0/8")}).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "http_request_duration_seconds")
	assert.Contains(t, body, `route="/observability-test-route"`)
	// The pool and runtime series are what make the endpoint worth scraping
	// when no request is failing.
	assert.Contains(t, body, "go_goroutines")
}

func TestHealthReportFailsOnlyOnARequiredDependency(t *testing.T) {
	t.Parallel()

	down := func(context.Context) error { return errors.New("connection refused") }
	up := func(context.Context) error { return nil }

	cases := []struct {
		name       string
		checks     []Check
		wantStatus string
		wantCode   int
	}{
		{
			name:       "all up",
			checks:     []Check{{Name: "database", Required: true, Probe: up}, {Name: "object_storage", Probe: up}},
			wantStatus: StatusOK,
			wantCode:   http.StatusOK,
		},
		{
			name:       "optional down",
			checks:     []Check{{Name: "database", Required: true, Probe: up}, {Name: "object_storage", Probe: down}},
			wantStatus: StatusDegraded,
			// Still 200: a deployment that can serve calendars without its
			// object store must not be pulled out of rotation for it.
			wantCode: http.StatusOK,
		},
		{
			name:       "required down",
			checks:     []Check{{Name: "database", Required: true, Probe: down}, {Name: "object_storage", Probe: up}},
			wantStatus: StatusDown,
			wantCode:   http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			NewReporter(0, tc.checks...).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/detail", nil))

			require.Equal(t, tc.wantCode, rec.Code)
			var got report
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
			assert.Equal(t, tc.wantStatus, got.Status)
			require.Len(t, got.Checks, len(tc.checks))
			// Sorted by name, so a scrape of an unchanged system is unchanged.
			assert.Equal(t, "database", got.Checks[0].Name)
		})
	}
}

// The report names why a dependency is down. That detail is the reason it is
// on the administrative listener and not on the public /health.
func TestHealthReportCarriesTheProbeError(t *testing.T) {
	t.Parallel()

	rep := NewReporter(0, Check{
		Name:     "database",
		Required: true,
		Probe:    func(context.Context) error { return errors.New("dial tcp 10.0.0.5:3306: connect: connection refused") },
	})

	rec := httptest.NewRecorder()
	rep.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/detail", nil))

	var got report
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	require.Len(t, got.Checks, 1)
	assert.Contains(t, got.Checks[0].Error, "connection refused")
	assert.Equal(t, StatusDown, got.Checks[0].Status)
}

// A probe that ignores its context must not be able to hold the report open:
// an endpoint that hangs when a dependency hangs cannot answer the one
// question it is asked.
func TestHealthReportIsBounded(t *testing.T) {
	t.Parallel()

	rep := NewReporter(50_000_000 /* 50ms */, Check{
		Name:     "database",
		Required: true,
		Probe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	status, results := rep.Run(context.Background())
	assert.Equal(t, StatusDown, status)
	require.Len(t, results, 1)
	assert.True(t, strings.Contains(results[0].Error, "deadline"),
		"expected the deadline to be what ended the probe, got %q", results[0].Error)
}
