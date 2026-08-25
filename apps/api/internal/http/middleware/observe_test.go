package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logCapture collects the JSON lines a logger writes, decoded.
type logCapture struct {
	buf bytes.Buffer
}

func (c *logCapture) logger() *slog.Logger {
	return slog.New(observability.NewContextHandler(
		slog.NewJSONHandler(&c.buf, &slog.HandlerOptions{Level: slog.LevelDebug}),
	))
}

func (c *logCapture) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	dec := json.NewDecoder(bytes.NewReader(c.buf.Bytes()))
	for dec.More() {
		var m map[string]any
		require.NoError(t, dec.Decode(&m))
		out = append(out, m)
	}
	return out
}

// observedRouter builds the stack under test around a chi router, because the
// route pattern the log and the metrics are keyed on only exists once chi has
// matched the request.
func observedRouter(logger *slog.Logger, trusted []netip.Prefix, register func(chi.Router)) http.Handler {
	r := chi.NewRouter()
	r.Use(RequestID(trusted))
	r.Use(Observe(logger))
	register(r)
	return r
}

func doGet(handler http.Handler, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = "198.51.100.7:44000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The paths this application serves carry credentials: a share token is a path
// segment, and a password reset carries its token in the query. A log that
// recorded the URL would file working credentials in the one artifact that is
// copied to third parties, kept longest, and read by people who were never
// given the calendar.
func TestAccessLogRecordsTheRouteAndNeverTheCredentialsInTheURL(t *testing.T) {
	t.Parallel()

	var cap logCapture
	h := observedRouter(cap.logger(), nil, func(r chi.Router) {
		r.Get("/share/{token}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	secret := "s3cr3t-share-token"
	resetToken := "reset-token-in-the-query"
	doGet(h, "/share/"+secret+"?token="+resetToken, nil)

	lines := cap.lines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, "/share/{token}", lines[0]["route"])

	raw := cap.buf.String()
	assert.NotContains(t, raw, secret, "the share token reached the log")
	assert.NotContains(t, raw, resetToken, "the reset token reached the log")
	assert.NotContains(t, raw, "?", "a query string reached the log")
}

// A path that matched no route is collapsed to one label. The set of paths
// that match nothing is whatever a client cares to type, and every distinct
// label is a time series the process keeps for as long as it runs.
func TestUnmatchedPathsShareOneRouteLabel(t *testing.T) {
	t.Parallel()

	// Mounted the way the served stack mounts its routes, because that is
	// what decides the answer: under a mount, an unmatched path is matched by
	// the mount's own catch-all and the router reports "/*" rather than
	// nothing at all.
	var cap logCapture
	h := observedRouter(cap.logger(), nil, func(r chi.Router) {
		inner := chi.NewRouter()
		inner.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		r.Mount("/", inner)
	})

	doGet(h, "/wp-admin/setup-config.php", nil)
	doGet(h, "/.env", nil)

	lines := cap.lines(t)
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Equal(t, "unmatched", line["route"])
		assert.EqualValues(t, http.StatusNotFound, line["status"])
	}
	assert.NotContains(t, cap.buf.String(), "wp-admin")
}

func TestAccessLogLevelFollowsTheStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path      string
		status    int
		wantLevel string
	}{
		{"/ok", http.StatusOK, "INFO"},
		{"/refused", http.StatusForbidden, "WARN"},
		{"/broken", http.StatusInternalServerError, "ERROR"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			var cap logCapture
			h := observedRouter(cap.logger(), nil, func(r chi.Router) {
				r.Get(tc.path, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
			})
			doGet(h, tc.path, nil)

			lines := cap.lines(t)
			require.Len(t, lines, 1)
			assert.Equal(t, tc.wantLevel, lines[0]["level"])
			assert.EqualValues(t, tc.status, lines[0]["status"])
		})
	}
}

// A liveness probe answering normally is the most frequent request a
// deployment serves and the least informative. A failing one is the opposite.
func TestHealthProbesAreLoggedOnlyWhenTheyFail(t *testing.T) {
	t.Parallel()

	t.Run("healthy is silent", func(t *testing.T) {
		t.Parallel()
		var cap logCapture
		h := observedRouter(cap.logger(), nil, func(r chi.Router) {
			r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		})
		doGet(h, "/health", nil)
		assert.Empty(t, cap.lines(t))
	})

	t.Run("unhealthy is logged", func(t *testing.T) {
		t.Parallel()
		var cap logCapture
		h := observedRouter(cap.logger(), nil, func(r chi.Router) {
			r.Get("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
		})
		doGet(h, "/health", nil)
		require.Len(t, cap.lines(t), 1)
	})
}

// The identifier is what ties an access-log line to the error a handler logged
// while serving it. Both have to carry it or neither is useful.
func TestRequestIDReachesTheResponseAndEveryLineTheRequestLogs(t *testing.T) {
	t.Parallel()

	var cap logCapture
	logger := cap.logger()
	h := observedRouter(logger, nil, func(r chi.Router) {
		r.Get("/work", func(w http.ResponseWriter, req *http.Request) {
			logger.ErrorContext(req.Context(), "something went wrong deep inside")
			w.WriteHeader(http.StatusInternalServerError)
		})
	})

	rec := doGet(h, "/work", nil)

	id := rec.Header().Get(observability.RequestIDHeader)
	require.NotEmpty(t, id, "the client was given no identifier to quote in a bug report")

	lines := cap.lines(t)
	require.Len(t, lines, 2)
	for _, line := range lines {
		assert.Equal(t, id, line["request_id"],
			"a line logged while serving the request is not attributable to it")
	}
}

func TestInboundRequestIDIsAdoptedOnlyFromATrustedProxy(t *testing.T) {
	t.Parallel()

	trusted := []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}
	supplied := "upstream-id-1234"

	cases := []struct {
		name     string
		trusted  []netip.Prefix
		header   string
		wantKept bool
	}{
		// A client that picks its own identifier can reuse one, and two
		// unrelated requests sharing an id is worse than neither having the
		// proxy's.
		{"untrusted peer is ignored", nil, supplied, false},
		{"trusted peer is adopted", trusted, supplied, true},
		{"trusted peer supplying junk is ignored", trusted, "id with spaces\nand a newline", false},
		{"trusted peer supplying nothing", trusted, "", false},
		{"trusted peer supplying an over-long id", trusted, longID(), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := observedRouter(nil, tc.trusted, func(r chi.Router) {
				r.Get("/work", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			})

			headers := map[string]string{}
			if tc.header != "" {
				headers[observability.RequestIDHeader] = tc.header
			}
			rec := doGet(h, "/work", headers)

			got := rec.Header().Get(observability.RequestIDHeader)
			require.NotEmpty(t, got, "every response carries an identifier, adopted or minted")
			if tc.wantKept {
				assert.Equal(t, tc.header, got)
			} else {
				assert.NotEqual(t, tc.header, got)
			}
		})
	}
}

// longID returns an identifier past the accepted length.
func longID() string {
	b := make([]byte, 65)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// The measurement sits outside panic recovery so that the request that
// panicked is counted, and counted as the 500 the client received. Measured
// from inside, the status would be whatever had been written before the panic
// -- which is nothing.
func TestAPanickedRequestIsLoggedWithTheStatusTheClientGot(t *testing.T) {
	t.Parallel()

	var cap logCapture
	r := chi.NewRouter()
	r.Use(RequestID(nil))
	r.Use(Observe(cap.logger()))
	r.Use(Recoverer())
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("handler exploded") })

	rec := doGet(r, "/boom", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	lines := cap.lines(t)
	require.Len(t, lines, 1)
	assert.EqualValues(t, http.StatusInternalServerError, lines[0]["status"])
	assert.Equal(t, "/boom", lines[0]["route"])
}

// Nil logger is how the test suites silence the access log. It must silence
// the log only: a server built that way is still measured, or every test-built
// server would be a hole in the metrics it is supposed to exercise.
func TestObserveWithoutALoggerStillMeasures(t *testing.T) {
	t.Parallel()

	before := scrapeCount(t, `route="/counted"`)

	h := observedRouter(nil, nil, func(r chi.Router) {
		r.Get("/counted", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	doGet(h, "/counted", nil)

	assert.Greater(t, scrapeCount(t, `route="/counted"`), before)
}

// scrapeCount counts the exported lines mentioning needle.
func scrapeCount(t *testing.T, needle string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	observability.MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	count := 0
	for _, line := range bytes.Split(rec.Body.Bytes(), []byte("\n")) {
		if bytes.Contains(line, []byte(needle)) {
			count++
		}
	}
	return count
}
