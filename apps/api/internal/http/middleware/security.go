package middleware

import "net/http"

// apiCSP is the policy for every JSON response: nothing may load or run.
const apiCSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// docsCSP is the policy for the one HTML page the API serves, the /docs
// reference. It admits the pinned renderer the page loads and the schema it
// fetches from this origin, and nothing else.
const docsCSP = "default-src 'none'; " +
	"script-src https://unpkg.com/@stoplight/elements@9.0.15/web-components.min.js; " +
	"style-src https://unpkg.com/@stoplight/elements@9.0.15/styles.min.css 'unsafe-inline'; " +
	"img-src 'self' data:; font-src data:; connect-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// SecurityHeaders sets a conservative set of headers for the API. JSON
// responses get a CSP that permits nothing; the /docs HTML page gets one
// permitting exactly what it loads. The SPA's own CSP is set by its host
// (e.g. CDN / nginx) on the HTML response.
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
			h.Set("Cross-Origin-Resource-Policy", "same-site")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if r.URL.Path == "/docs" {
				h.Set("Content-Security-Policy", docsCSP)
			} else {
				h.Set("Content-Security-Policy", apiCSP)
			}
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			next.ServeHTTP(w, r)
		})
	}
}
