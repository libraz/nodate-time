package observability

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both label helpers exist for the same reason: a label taken from the request
// is attacker-controlled, and every distinct value is a time series the
// process holds for as long as it runs. One scan is enough to make a registry
// that never shrinks.
func TestLabelsAreBounded(t *testing.T) {
	t.Parallel()

	t.Run("method", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, http.MethodGet, NormalizeMethod(http.MethodGet))
		assert.Equal(t, http.MethodDelete, NormalizeMethod(http.MethodDelete))
		assert.Equal(t, "other", NormalizeMethod("PROPFIND"))
		assert.Equal(t, "other", NormalizeMethod(strings.Repeat("X", 4096)))
		assert.Equal(t, "other", NormalizeMethod(""))
	})

	t.Run("route", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/calendars/{id}/events", NormalizeRoute("/calendars/{id}/events"))
		// Both spellings of "nothing matched": a bare router reports the
		// empty pattern, and the served stack reports the catch-all of the
		// mount the routes hang off.
		assert.Equal(t, "unmatched", NormalizeRoute(""))
		assert.Equal(t, "unmatched", NormalizeRoute("/*"))
	})
}

func TestRecordedRequestsAreExported(t *testing.T) {
	t.Parallel()

	RecordRequest(http.MethodPost, "/metrics-test/created", http.StatusCreated, 0.02, 512)
	RecordRateLimitRejection("metrics-test-scope")
	RecordMailDelivery(MailFailed)

	body := scrape(t)

	assert.Contains(t, body, `http_requests_total{method="POST",route="/metrics-test/created",status="201"}`)
	assert.Contains(t, body, `http_response_bytes_total{method="POST",route="/metrics-test/created"}`)
	assert.Contains(t, body, `ratelimit_rejections_total{scope="metrics-test-scope"}`)
	assert.Contains(t, body, `mail_deliveries_total{result="failed"}`)
	// The gauge has to be present even at zero, or an alert on it never fires
	// on a quiet process -- it just has no series to evaluate.
	assert.Contains(t, body, "http_requests_in_flight")
}

// The in-flight gauge returns to where it started. A leak here reads as
// permanently rising concurrency, which is exactly the shape of the incident
// it would be consulted about.
func TestInFlightGaugeIsBalanced(t *testing.T) {
	t.Parallel()

	before := gaugeValue(t, "http_requests_in_flight")
	done := RequestStarted()
	during := gaugeValue(t, "http_requests_in_flight")
	done()
	after := gaugeValue(t, "http_requests_in_flight")

	assert.Equal(t, before+1, during)
	assert.Equal(t, before, after)
}

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	MetricsHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// gaugeValue reads one unlabelled gauge out of the exported text.
func gaugeValue(t *testing.T, name string) float64 {
	t.Helper()
	for _, line := range strings.Split(scrape(t), "\n") {
		if !strings.HasPrefix(line, name+" ") {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, name+" ")), 64)
		require.NoError(t, err)
		return v
	}
	t.Fatalf("%s is not exported", name)
	return 0
}
