// Package observability holds what the process reports about itself: the
// metrics it exports, the request identity that ties its log lines together,
// and the administrative listener the two are read from.
//
// It deliberately depends on nothing else in this application. The things it
// measures -- the HTTP stack, the mailer, the rate limiter -- import it, and a
// dependency in the other direction would make the measurement participate in
// what it measures.
package observability

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry is this process's own registry rather than the package-global
// default one. A library that registers a collector into the default registry
// would otherwise appear in this endpoint without anything here saying so, and
// what a deployment exports is part of its interface.
var registry = prometheus.NewRegistry()

// latencyBuckets ends at 30s because that is the server's write timeout: a
// request slower than the last bucket is one the server gave up on, and a
// histogram whose top bucket sits below that cannot show the difference
// between slow and abandoned.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

var (
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Requests served, by method, matched route and response status.",
	}, []string{"method", "route", "status"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Time to produce a response, by method and matched route.",
		Buckets: latencyBuckets,
	}, []string{"method", "route"})

	httpRequestsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Requests currently being served.",
	})

	httpResponseBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_response_bytes_total",
		Help: "Response body bytes written, by method and matched route.",
	}, []string{"method", "route"})

	// rateLimitRejections is what turns a rate limiter from a defence nobody
	// can see into one that can be alerted on: a credential-stuffing run and a
	// misconfigured TC_TRUSTED_PROXIES look identical in the response codes
	// clients receive, and differ here only in how many addresses are counted.
	rateLimitRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ratelimit_rejections_total",
		Help: "Requests refused by a rate limiter, by limiter scope.",
	}, []string{"scope"})

	// mailDeliveries exists because mail is sent in the background, so a relay
	// that stopped accepting messages costs no request an error and no user a
	// visible failure -- it silently removes password reset and email
	// verification from the deployment until someone notices.
	mailDeliveries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "mail_deliveries_total",
		Help: "Outgoing mail by outcome: sent, failed, or refused before it was queued.",
	}, []string{"result"})
)

func init() {
	registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		httpRequestsInFlight,
		httpResponseBytes,
		rateLimitRejections,
		mailDeliveries,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// Mail delivery outcomes, named here so the producer cannot invent a third
// spelling of the same result.
const (
	MailSent     = "sent"
	MailFailed   = "failed"
	MailRefused  = "refused"
	unmatchedRte = "unmatched"
	otherMethod  = "other"
)

// knownMethods bounds the method label. A label fed straight from the request
// line is attacker-controlled, and every distinct value is a permanent time
// series: one scan with random verbs would leave the registry carrying them
// for the life of the process.
var knownMethods = map[string]struct{}{
	http.MethodGet: {}, http.MethodHead: {}, http.MethodPost: {}, http.MethodPut: {},
	http.MethodPatch: {}, http.MethodDelete: {}, http.MethodOptions: {}, http.MethodConnect: {},
	http.MethodTrace: {},
}

// NormalizeMethod maps a request method onto the bounded label set.
func NormalizeMethod(method string) string {
	if _, ok := knownMethods[method]; ok {
		return method
	}
	return otherMethod
}

// NormalizeRoute maps a resolved route pattern onto the bounded label set.
//
// Two patterns mean "nothing matched" and both collapse to one series. The
// empty one is what a bare router reports. "/*" is what the served stack
// reports, because the routes are mounted at "/" and an unmatched path is
// therefore matched by the mount's own catch-all -- it is a route pattern in
// form only, and labelling it as one would put a line in the metrics that
// looks like an endpoint nobody wrote.
//
// Collapsing them is not what bounds the label -- the router already does
// that, by only ever reporting patterns it was given -- it is what makes the
// label legible. Should this application ever register a genuine wildcard
// route, it needs a different name here, not this line deleted.
func NormalizeRoute(pattern string) string {
	if pattern == "" || pattern == "/*" {
		return unmatchedRte
	}
	return pattern
}

// RequestStarted reports a request entering the served stack. The returned
// function must be called when it leaves.
func RequestStarted() func() {
	httpRequestsInFlight.Inc()
	return httpRequestsInFlight.Dec
}

// RecordRequest files one finished request. method and route are expected to
// have passed through the Normalize helpers.
func RecordRequest(method, route string, status int, seconds float64, responseBytes int64) {
	httpRequestsTotal.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	httpRequestDuration.WithLabelValues(method, route).Observe(seconds)
	if responseBytes > 0 {
		httpResponseBytes.WithLabelValues(method, route).Add(float64(responseBytes))
	}
}

// RecordRateLimitRejection files a request refused by the limiter named scope.
func RecordRateLimitRejection(scope string) {
	rateLimitRejections.WithLabelValues(scope).Inc()
}

// RecordMailDelivery files an outgoing message by outcome (see MailSent and
// its siblings).
func RecordMailDelivery(result string) {
	mailDeliveries.WithLabelValues(result).Inc()
}

// RegisterDBStats exports the connection pool of db under the given name.
//
// Pool saturation is the failure this exists for: when every connection is
// checked out, requests queue in the driver rather than in the server, so the
// handlers look idle, the CPU looks idle, and the only place the wait is
// visible is here.
//
// Registering the same name twice is reported rather than fatal, so a process
// that builds a second pool cannot be stopped by its own instrumentation.
func RegisterDBStats(name string, db *sql.DB) error {
	err := registry.Register(collectors.NewDBStatsCollector(db, name))
	if _, duplicate := err.(prometheus.AlreadyRegisteredError); duplicate {
		return nil
	}
	return err
}

// MetricsHandler serves the registry in the Prometheus text format.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		// A collector that panics takes down the scrape, not the process
		// being scraped.
		ErrorHandling: promhttp.ContinueOnError,
	})
}
