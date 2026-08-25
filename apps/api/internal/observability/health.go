package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Check is one dependency the process can be asked about.
type Check struct {
	// Name identifies the dependency in the report.
	Name string
	// Probe returns nil when the dependency is usable. It is expected to
	// respect the context: the report is bounded, and a probe that ignores
	// cancellation holds the whole report open.
	Probe func(ctx context.Context) error
	// Required marks a dependency the deployment cannot serve without. A
	// failing required check makes the report unhealthy; a failing optional
	// one only degrades it, because running without object storage or without
	// a mail relay are both supported configurations and neither should page
	// anyone at night.
	Required bool
}

// Report statuses.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDown     = "down"
)

type checkResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	// Error carries the probe's message. This is served on the administrative
	// listener only -- a dependency failure usually names a host, a bucket or
	// a credential, and that is exactly the detail the public /health must not
	// carry and the one this endpoint exists to give.
	Error string `json:"error,omitempty"`
}

type report struct {
	Status string        `json:"status"`
	Checks []checkResult `json:"checks"`
}

// Reporter answers a detailed health report over a fixed set of checks.
type Reporter struct {
	checks  []Check
	timeout time.Duration
}

// defaultProbeTimeout bounds the whole report. A health endpoint that hangs
// when a dependency hangs is indistinguishable from the process being dead,
// which is the one question it is asked to answer.
const defaultProbeTimeout = 5 * time.Second

// NewReporter builds a reporter over checks. A zero timeout selects the
// default.
func NewReporter(timeout time.Duration, checks ...Check) *Reporter {
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	return &Reporter{checks: checks, timeout: timeout}
}

// Run executes every check concurrently under a shared deadline and returns
// the overall status alongside the individual results, sorted by name so two
// scrapes of an unchanged system produce an unchanged document.
func (rep *Reporter) Run(ctx context.Context) (string, []checkResult) {
	ctx, cancel := context.WithTimeout(ctx, rep.timeout)
	defer cancel()

	results := make([]checkResult, len(rep.checks))
	var wg sync.WaitGroup
	for i, c := range rep.checks {
		wg.Add(1)
		go func(i int, c Check) {
			defer wg.Done()
			start := time.Now()
			err := c.Probe(ctx)
			res := checkResult{Name: c.Name, Status: StatusOK, DurationMS: time.Since(start).Milliseconds()}
			if err != nil {
				res.Status = StatusDown
				res.Error = err.Error()
			}
			results[i] = res
		}(i, c)
	}
	wg.Wait()

	status := StatusOK
	for i, c := range rep.checks {
		if results[i].Status == StatusOK {
			continue
		}
		if c.Required {
			status = StatusDown
			break
		}
		status = StatusDegraded
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return status, results
}

// Handler serves the report as JSON. It answers 503 only when a required
// dependency is down: a degraded report is still a process that can serve, and
// a load balancer that pulls it out of rotation over an unreachable mail relay
// has turned a partial outage into a total one.
func (rep *Reporter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, results := rep.Run(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if status == StatusDown {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(report{Status: status, Checks: results})
	})
}
