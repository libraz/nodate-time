# API overview

Huma v2 generates the OpenAPI description automatically. It is served without authentication at `/openapi.json` and `/openapi.yaml`, with a rendered reference at `/docs`. Routes live in `apps/api/internal/http/router/router.go`.

The groups below are not exhaustive; the router is the full list.

- **Public**: `/health`, `/openapi.json`, `/openapi.yaml`, `/docs`; `/auth/*` except `/auth/logout` (register, login, refresh, password-reset, verify-email/confirm, oauth; `dev-login` only in development); `/share/{token}` and `/share/{token}/events`; `/blobs/*`, which carries no session and is authorized by the signature on the URL.
- **Authenticated**: `/auth/logout`, `/user/*`, `/calendars`, `/calendars/{id}/{events,memos,members,invites,albums,labels,activity,export,import}`, and `/invites/{token}/accept`.
- **Admin**: `/admin/oauth-providers`, `/admin/allowed-emails`, `/admin/instance-admins`.

Sign-in, registration, password reset and OAuth (`/auth/*`) and the public share links (`/share/*`) are rate-limited per client IP; see `TC_AUTH_RATE_LIMIT` and `TC_SHARE_RATE_LIMIT` in [Configuration](configuration.md). `/health`, the OpenAPI and docs routes and `/blobs/*` are not rate-limited.
