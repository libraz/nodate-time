package router

import (
	"database/sql"
	"net/http"
	"net/netip"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/http/blobs"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/events"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/users"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/mailer"
	"github.com/libraz/nodate-time/apps/api/internal/secrets"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

type Deps struct {
	DB        *sql.DB
	Queries   *generated.Queries
	JWTSecret string
	// WorkspaceID is the single workspace this deployment serves. It is
	// resolved at startup and threaded into every handler, because the
	// shared schema scopes rows by it and a query that leaves it at zero
	// matches nothing rather than failing loudly.
	WorkspaceID uint32
	// WorkspacePublicID is the workspace's external id. Object storage keys
	// are built from it, so it has to be the same scope the storage_objects
	// unique key uses -- see events.PresignUpload.
	WorkspacePublicID []byte
	Storage           *storage.Client
	// Signer mints and checks the URLs the browser reads and writes blobs
	// through. It is the same signer the storage client mints with, because a
	// URL minted by one and checked by the other would never verify.
	Signer *storage.Signer
	Mailer mailer.Mailer
	WebURL string
	// AppName is what the deployment calls itself: the title of the OpenAPI
	// document, the product named in outgoing mail, and the PRODID of an
	// exported calendar. Empty applies defaultAppName, so a caller that does
	// not care -- every test server -- does not have to name it.
	AppName string
	// AccountDefaults seed the locale and timezone of an account created by
	// signing up or by a first provider sign-in. Neither collects them, and
	// both columns are NOT NULL. Zero applies the software's own defaults.
	AccountDefaults users.AccountDefaults
	// APIPublicURL is the address this API answers at from outside, including
	// any path a reverse proxy serves it under. The $schema links in responses
	// are built from the path part: they are followed by a client that only
	// knows the public address, so a link naming the API's own root leads
	// nowhere when the API is published under a prefix. See openapi.go.
	APIPublicURL string
	OAuth        users.OAuthConfig
	Cipher       *secrets.Cipher
	// GoogleAllowedDomains restricts sign-up and sign-in (password registration
	// and login, Google) to these email domains, bar addresses an admin
	// allow-listed individually. LINE verifies no address, so it is refused
	// while any restriction is set.
	// Empty means unrestricted. See config.GoogleAllowedDomainList.
	GoogleAllowedDomains []string
	// DevMode enables development-only endpoints (e.g. /auth/dev-login).
	DevMode bool
	// PasswordLoginEnabled gates the email+password auth routes (register,
	// login, password reset). When false, only OAuth/OIDC sign-in is offered.
	PasswordLoginEnabled bool
	// AuthRateLimit sets the per-IP request budget for sensitive unauthenticated
	// endpoints. Zero applies the default (60/min); a negative value disables the
	// limiter entirely (used by parallel integration tests).
	AuthRateLimit int
	// ShareRateLimit is the budget for public share links, counted separately.
	//
	// A share link is meant to be opened by many people, and one page view
	// costs more than one request. Counted against the sign-in budget, an
	// ordinary audience behind one address exhausts the limit that exists to
	// slow down credential guessing -- and locks out sign-in for everyone
	// sharing that address. Zero applies the default; negative disables.
	ShareRateLimit int
	// DAVRateLimit is the budget for CalDAV requests, counted separately: a
	// client syncing several calendars sends far more requests than a person
	// browsing. Zero applies the default; negative disables.
	DAVRateLimit int
	// TrustedProxies lists reverse-proxy hops allowed to set X-Forwarded-For for
	// per-client rate limiting. Nil trusts no proxy: RemoteAddr is always used.
	// See config.Config.TrustedProxyList.
	TrustedProxies []netip.Prefix
}

// Default per-IP budgets, per minute. Sign-in is deliberately tight: the
// requests it covers are the ones worth guessing at. A share link is opened by
// an audience rather than an attacker, so its budget is sized for a page view
// costing more than one request.
const (
	defaultAuthRateLimit  = 60
	defaultShareRateLimit = 600
	defaultDAVRateLimit   = 600
)

// What the deployment is called when it has not been told. The SPA falls back
// to the same name, in apps/web/src/lib/brand-rules.ts.
const defaultAppName = "Nodate Time"

// apiGroups holds one huma API per route group. Each is mounted on the same
// route tree with its own middleware and writes into the one shared document.
type apiGroups struct {
	public    huma.API
	share     huma.API
	protected huma.API
	admin     huma.API
}

func appNameOf(deps Deps) string {
	if deps.AppName == "" {
		return defaultAppName
	}
	return deps.AppName
}

func newUserDeps(deps Deps) users.Deps {
	return users.Deps{DB: deps.DB, Queries: deps.Queries, JWTSecret: deps.JWTSecret, Storage: deps.Storage, WorkspaceID: deps.WorkspaceID, AllowedDomains: deps.GoogleAllowedDomains, Mailer: deps.Mailer, WebURL: deps.WebURL, AppName: appNameOf(deps), Defaults: deps.AccountDefaults}
}

func newEventDeps(deps Deps) events.Deps {
	return events.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage, WorkspaceID: deps.WorkspaceID, WorkspacePublicID: deps.WorkspacePublicID}
}

// BuildResult returns the handler together with the OpenAPI document every
// registered operation was written into.
func BuildResult(deps Deps) (http.Handler, *huma.OpenAPI) {
	r := chi.NewRouter()
	r.Use(middleware.ClientIPMiddleware(deps.TrustedProxies))

	// One document, three groups: see openapi.go for why the config is shared
	// and the document routes live on the bare mux.
	apiConfig := docConfig(appNameOf(deps), "1.0.0", deps.APIPublicURL)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Blob relay. On the bare mux for the same reason /health is: it is not
	// part of the typed API surface, it is addressed only by URLs the API
	// itself minted, and it carries raw bytes rather than a JSON body huma
	// would parse and cap. No auth middleware -- the URL's own signature is
	// what authorizes it. See the blobs package.
	if deps.Signer != nil {
		r.Mount(storage.BlobPath, blobs.Handler(blobs.Deps{Storage: deps.Storage, Signer: deps.Signer}))
	}

	// CalDAV. On the bare mux because it speaks WebDAV methods and XML rather
	// than the typed JSON API, and authenticates with HTTP Basic itself.
	davLimit := deps.DAVRateLimit
	if davLimit == 0 {
		davLimit = defaultDAVRateLimit
	}
	mountDAV(r, deps, newEventDeps(deps), davLimit)

	// --- Public routes (no auth) ---
	// Sensitive unauthenticated endpoints (login, register, password reset,
	// OAuth, public share) are rate-limited per client IP to blunt credential
	// brute-force and password-reset mail-bombing.
	authLimit := deps.AuthRateLimit
	if authLimit == 0 {
		authLimit = defaultAuthRateLimit
	}
	shareLimit := deps.ShareRateLimit
	if shareLimit == 0 {
		shareLimit = defaultShareRateLimit
	}
	var g apiGroups
	r.Group(func(pub chi.Router) {
		if authLimit > 0 {
			pub.Use(middleware.NewRateLimiter("auth", authLimit, time.Minute, deps.TrustedProxies).Middleware())
		}
		g.public = humachi.New(pub, apiConfig)
	})
	r.Group(func(share chi.Router) {
		if shareLimit > 0 {
			share.Use(middleware.NewRateLimiter("share", shareLimit, time.Minute, deps.TrustedProxies).Middleware())
		}
		g.share = humachi.New(share, apiConfig)
	})
	r.Group(func(prot chi.Router) {
		prot.Use(middleware.RequireAuth(deps.JWTSecret, deps.Queries))
		g.protected = humachi.New(prot, apiConfig)
	})
	r.Group(func(adm chi.Router) {
		adm.Use(middleware.RequireAuth(deps.JWTSecret, deps.Queries))
		adm.Use(middleware.RequireAdmin(deps.Queries))
		g.admin = humachi.New(adm, apiConfig)
	})

	registerAuth(g, deps)
	registerSchedulePolls(g, deps)
	registerUsers(g, deps)
	registerAppPasswords(g, deps)
	registerCalendars(g, deps)
	registerEvents(g, deps)
	registerEventRestore(g, deps)
	registerSearch(g, deps)
	registerAudit(g, deps)
	registerEventDetails(g, deps)
	registerAlbums(g, deps)
	registerMemos(g, deps)
	registerInvites(g, deps)
	registerAdmin(g, deps)

	// Last, so the document is complete and so these registrations are the
	// ones the shared route tree keeps.
	serveDocs(r, apiConfig)

	return r, apiConfig.OpenAPI
}
