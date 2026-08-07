package app

import (
	"net/http"
	"time"
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
}

// DefaultTimeouts is what the deployment serves with. Write is the loosest of
// the three because a response may be an export or an import report, which is
// built before it is sent.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		Read:  15 * time.Second,
		Write: 30 * time.Second,
		Idle:  60 * time.Second,
	}
}

// NewServer builds the http.Server the process runs, so that the limits it
// serves under are defined once rather than only inside main -- where nothing
// can reach them and a value that quietly went missing would look fine.
func NewServer(addr string, handler http.Handler, timeouts Timeouts) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  timeouts.Read,
		WriteTimeout: timeouts.Write,
		IdleTimeout:  timeouts.Idle,
	}
}
