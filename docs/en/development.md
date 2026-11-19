# Development

## Tech stack

| Area            | Technology                                                                              |
| --------------- | --------------------------------------------------------------------------------------- |
| API             | Go 1.27.4 + [Huma v2](https://huma.rocks) (OpenAPI) + chi/v5 + [sqlc](https://sqlc.dev) |
| DB              | MySQL 26.10.0 (Docker Compose)                                                          |
| Object storage  | S3-compatible (Versity S3 Gateway locally)                                              |
| Web             | React 19 + TypeScript 7 + Vite + TanStack Router + Zustand + Tailwind CSS 4 + Luxon     |
| Lint / format   | Biome (web) / gofmt (Go)                                                                |
| Package manager | Bun 1.4.5                                                                               |
| Testing         | Go E2E (testify + real MySQL) / Vitest (web)                                            |

## Repo structure

```
apps/
  api/                 Go REST API
    cmd/api/           entrypoint
    cmd/createuser/    user-creation CLI
    cmd/healthcheck/   container health probe
    internal/          config, auth, cleanup, db/generated, errors,
                       eventlog, mailer, recurrence, secrets, storage,
                       http/{handlers,middleware,router}
    tests/e2e/         E2E tests
  web/                 React SPA
    src/components/     UI components
    src/routes/         TanStack Router (file-based; incl. share/ and embed/)
    src/stores/         Zustand (auth / calendar / ui stores)
    src/lib/            utilities (date, recurrence, upload, theme, …)
    src/i18n/           English / Japanese locales
sql/
  core/                vendored shared schema contract (pinned; not edited here)
    tables/             shared table definitions
    triggers/           shared triggers
    conformance/        contract conformance checks
    tools/              schema build, vendoring and layer checks
    upgrades/           bring an existing database's shared tables up to date
  time/                this application's own tables
    tables/             table definitions
    views/              view definitions
    upgrades/           bring an existing database's own tables and rows up to date
  queries/             sqlc query definitions
  schema.sql           combined schema produced by build-schema.sh
  sqlc.yaml            sqlc config
compose.yml            local stack: MySQL + object storage
compose.prod.yml       self-hosting stack: MySQL + object storage + API + web
```

[Deployment](deployment.md) shows how the production stack is wired together.

## Testing

E2E tests create an independent user and calendar per test (tenant isolation) and run in parallel against a real MySQL.

```sh
make db-up                      # MySQL on TC_DB_PORT (default 33306)
make test-e2e                   # runs with TC_TEST_INTEGRATION=1
```

The tests connect to `127.0.0.1:${TC_DB_PORT:-33306}`, and so does CI, so a non-default `TC_DB_PORT` has to be exported for both commands.

CI (`.github/workflows/ci.yml`) runs web (Biome / typecheck / Vitest) and API (build / vet / E2E).

## Migrations and code generation

- The schema is layered. `sql/core/` is a copy of the shared schema contract that lets more than one product write to the same database; it is not edited here and is pinned upstream in `sql/core/UPSTREAM.json`. `sql/time/` holds this application's own tables.
- `sql/core/tools/build-schema.sh core time` concatenates core then time into `sql/schema.sql` (`make db-schema`), which `make db-apply` applies to an empty database.
- An existing database is upgraded rather than rebuilt; see [Upgrading](deployment.md#upgrading). `bash scripts/check-upgrade-path.sh` checks that the upgrades take a v0.1.0 database, with data, to the current schema.
- `make test-conformance` checks the resulting schema against the contract. `make check-core` checks that the vendored copy is unedited and current.
- Write queries in `sql/queries/*.sql` and run `make sqlc` to generate Go code into `apps/api/internal/db/generated/`.
