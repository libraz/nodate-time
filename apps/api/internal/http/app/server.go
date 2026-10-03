package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// Timeouts bound how long one connection may occupy the server. They are as
// much part of the served stack as the middleware is: a request that never
// finishes arriving, or a client that reads its response one byte at a time,
// costs a connection and a goroutine for as long as it is allowed to.
type Timeouts struct {
	// Read is the budget for the whole request, headers and body.
	Read time.Duration
	// Write is the budget for producing the response.
	Write time.Duration
	// Idle is how long a kept-alive connection may sit between requests.
	Idle time.Duration
	// Transfer replaces Read and Write for the blob relay, whose bodies run
	// to the size an upload was granted. It matches the reverse proxy's own
	// transfer budget.
	Transfer time.Duration
}

// DefaultTimeouts is what the deployment serves with. Write is the loosest of
// the general limits because a response may be an export or an import report, which is
// built before it is sent.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Read:  15 * time.Second,
		Write: 30 * time.Second,
		Idle:  60 * time.Second,
		// nginx's proxy_read_timeout / proxy_send_timeout for /api.
		Transfer: 300 * time.Second,
	}
}

// NewServer builds the http.Server the process runs, so that the limits it
// serves under are defined once rather than only inside main -- where nothing
// can reach them and a value that quietly went missing would look fine.
func NewServer(addr string, handler http.Handler, timeouts Timeouts) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      allowTransfers(handler, timeouts.Transfer),
		ReadTimeout:  timeouts.Read,
		WriteTimeout: timeouts.Write,
		IdleTimeout:  timeouts.Idle,
	}
}

// allowTransfers extends both deadlines of a blob relay request to budget.
// Every other route keeps the server-wide limits, so a slow client still
// cannot hold a connection open on them.
func allowTransfers(next http.Handler, budget time.Duration) http.Handler {
	if budget <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == storage.BlobPath || strings.HasPrefix(r.URL.Path, storage.BlobPath+"/") {
			rc := http.NewResponseController(w)
			deadline := time.Now().Add(budget)
			_ = rc.SetReadDeadline(deadline)
			_ = rc.SetWriteDeadline(deadline)
		}
		next.ServeHTTP(w, r)
	})
}
