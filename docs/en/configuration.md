# Configuration

All variables are prefixed with `TC_`. The table lists the key ones; `apps/api/internal/config/config.go` has the full list and the defaults. [Deployment](deployment.md) describes which of them `compose.prod.yml` passes through and which it fixes.

| Variable | Purpose | Default |
| --- | --- | --- |
| `TC_ENV` | `development` enables dev conveniences | `production` |
| `TC_APP_NAME` | what the deployment calls itself (OpenAPI title, outgoing mail, iCalendar `PRODID`); the SPA takes the same name from the `VITE_APP_NAME` build argument, which `compose.prod.yml` fills from this variable. At most 64 characters; control characters, line and bidirectional separators, and `:` are refused at startup and at build time. Blank means the default | `Nodate Time` |
| `TC_APP_LOGO_URL` | logo shown on the sign-in screens and as the favicon: an `https` URL or a `data:image/` URI (png, svg+xml, webp, jpeg, gif) of at most 32,768 characters. Compiled into the SPA through the `VITE_APP_LOGO_URL` build argument, so changing it means rebuilding the web image; the image's CSP allows the logo's host automatically | none |
| `TC_PORT` | API port | `8080` |
| `TC_DB_DSN` | MySQL DSN | `ttuser:ttpw@tcp(127.0.0.1:33306)/timetree_clone?parseTime=true` |
| `TC_JWT_SECRET` | JWT signing key, at least 32 bytes. Required in every environment: the built-in value is published in this repository and is refused | none (`make dev` generates one under `secrets/`) |
| `TC_PASSWORD_LOGIN_ENABLED` | enable email+password auth | `true` |
| `TC_WORKSPACE_TIMEZONE` | IANA zone a new account starts in. Neither signing up nor a first provider sign-in asks, and the column is NOT NULL. Rejected at startup if the tz database does not know it | `Asia/Tokyo` |
| `TC_DEFAULT_LOCALE` | BCP 47 tag a new account starts with, for the same reason | `ja` |
| `TC_CORS_ALLOWED_ORIGINS` | allowed CORS origins, comma-separated. An empty list or `*` is refused in every environment | localhost:5173 |
| `TC_TRUSTED_PROXIES` | reverse-proxy hops allowed to set `X-Forwarded-For` (CIDR or bare IP, comma-separated). Unset behind a proxy collapses every client onto one rate-limit bucket | none trusted |
| `TC_AUTH_RATE_LIMIT` | per-IP requests/min for sign-in, registration, password reset and OAuth | `60` |
| `TC_SHARE_RATE_LIMIT` | per-IP requests/min for public share links, counted separately from sign-in | `600` |
| `TC_WEB_URL` / `TC_API_PUBLIC_URL` | public URLs for web / API. `TC_API_PUBLIC_URL` is also the address blob URLs point back at, so it must include any path the API is served under | localhost |
| `TC_S3_ENDPOINT` / `TC_S3_ACCESS_KEY` / `TC_S3_SECRET_KEY` / `TC_S3_BUCKET` / `TC_S3_USE_SSL` | S3-compatible storage, reached by the API only and never by the browser. The `minioadmin` credentials are refused unless `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` is set | local storage defaults |
| `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` | permit the well-known `minioadmin` credentials, for a local object store | `false` (`make dev` sets `true`) |
| `TC_SMTP_HOST` and other `TC_SMTP_*` | mail delivery. Empty means mail is off, in every environment: the API starts, logs that delivery is off, and does not offer password reset or email verification (the auth-providers response carries `mailEnabled: false`, and the SPA hides those screens) | unset (`make dev` points it at mailpit) |
| `TC_GOOGLE_CLIENT_ID` / `TC_GOOGLE_CLIENT_SECRET` | Google OAuth | empty |
| `TC_GOOGLE_ALLOWED_DOMAINS` | email domains allowed to sign up or sign in (comma-separated). Password registration and login and Google sign-in check it; LINE verifies no address, so it is refused while the list is set. An address outside the list still gets in if it was allow-listed individually, and `createuser` allow-lists the accounts it creates | unrestricted |
| `TC_LINE_CLIENT_ID` / `TC_LINE_CLIENT_SECRET` | LINE OAuth | empty |
| `TC_SECRETS_KEY` | key encrypting secrets stored in the DB (hex/base64, 32 bytes) | empty |
| `TC_METRICS_ADDR` | address of the administrative listener; empty disables it | `127.0.0.1:9464` |
| `TC_METRICS_ALLOWED_CIDRS` | networks allowed to reach that listener, matched against the direct TCP peer. Empty denies everything | loopback + private ranges |

## Startup validation

`Config.Validate()` applies in every environment, `TC_ENV=development` included. It refuses to start with:

- the built-in JWT secret, or one shorter than 32 bytes;
- the default `minioadmin` credentials;
- an empty or wildcard CORS list;
- a `TC_WORKSPACE_TIMEZONE` the tz database does not know.

One guard has an allowance variable: `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` for the storage credentials. The others have none. `TC_ENV=development` adds `/auth/dev-login` and nothing else.

## Local stack variables

`compose.yml` reads `TC_DB_PORT` (default 33306), `TC_DB_ROOT_PASSWORD`, `TC_DB_NAME`, `TC_DB_USER`, `TC_DB_PASSWORD`, `TC_S3_PORT` (default 9000, bound to loopback), `TC_MAILPIT_SMTP_PORT` (default 1025) and `TC_MAILPIT_UI_PORT` (default 8025). Both mailpit ports are bound to loopback.
