package observability

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"net/netip"
	"time"
)

// AdminOptions configures the administrative listener.
type AdminOptions struct {
	// AllowedPeers lists the networks permitted to reach this handler. An
	// empty list denies everything -- see peerAllowed for why that is the
	// safe reading of "nothing configured" and the permissive one is not.
	AllowedPeers []netip.Prefix
	// Health, when non-nil, is served at /health/detail.
	Health *Reporter
}

// NewAdminHandler builds the handler the administrative listener serves:
// metrics, profiles, and the detailed health report.
//
// Everything on it answers a question about the process rather than about the
// product, and everything on it is more than a client should be able to learn:
// the metrics enumerate the routes and their traffic, and the profiles carry
// fragments of live memory. It is therefore served on its own listener, bound
// by default to the loopback interface, and gated a second time on the peer
// address. Either guard alone would be enough on a correct deployment; the
// second one is there for the deployment that is not.
func NewAdminHandler(opts AdminOptions) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /metrics", MetricsHandler())

	// Referenced explicitly rather than by serving http.DefaultServeMux, which
	// importing net/http/pprof also populates. Mounting that mux would export
	// whatever any dependency has registered on it, which is not a list this
	// file can see or keep.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	if opts.Health != nil {
		mux.Handle("GET /health/detail", opts.Health.Handler())
	}

	return allowPeers(opts.AllowedPeers, mux)
}

// allowPeers refuses any request whose direct TCP peer is outside allowed.
//
// The peer address is the only thing consulted. X-Forwarded-For is not read
// here at any trust level, because this listener is not meant to be reached
// through a proxy: the moment a forwarded header could name the client, a
// misrouted rule on a public reverse proxy would be enough to publish the
// profiles.
func allowPeers(allowed []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !peerAllowed(r.RemoteAddr, allowed) {
			slog.WarnContext(r.Context(), "administrative request refused", "peer", r.RemoteAddr, "path", r.URL.Path)
			// 404 rather than 403: a refusal that distinguishes "not allowed
			// here" from "not here" confirms the endpoint exists, which is the
			// first thing anyone probing for it wants to know.
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// peerAllowed reports whether remoteAddr falls inside one of allowed.
func peerAllowed(remoteAddr string, allowed []netip.Prefix) bool {
	if len(allowed) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	// A connection to an IPv4 address on a dual-stack listener arrives as the
	// IPv4-mapped IPv6 form, which no IPv4 prefix contains. Unmapped, an
	// operator's 127.0.0.0/8 means what they wrote.
	addr = addr.Unmap()
	for _, p := range allowed {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// adminWriteTimeout is deliberately long. A CPU profile is a response that
// takes as long as the profile was asked to run -- 30 seconds by default and
// often more -- so the timeout that protects the public listener would here
// cut off the answer every time it was worth asking for.
const adminWriteTimeout = 6 * time.Minute

// NewAdminServer builds the server the administrative handler runs on.
func NewAdminServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: adminWriteTimeout,
		IdleTimeout:  60 * time.Second,
	}
}
