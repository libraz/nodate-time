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
    cmd/importtimetree/ TimeTree importer
    cmd/openapi/       writes the OpenAPI description
    internal/          domain packages (auth, dav, ical, recurrence, storage,
                       timetree, …), db/generated and
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
    constraints/        foreign keys and checks added after the tables
    triggers/           this layer's triggers
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
make storage-up                 # object storage, for the storage run
make test-e2e-storage           # adds the tests that need object storage
make test-e2e-storage-absent    # the tests for the 503 without object storage
```

Without `TC_TEST_STORAGE`, the attachment, album, avatar and cover tests skip and `make test-e2e` still passes, so count the skips rather than trusting a green run. The tests for absent object storage skip when it is set. The test server is built once per process, so the two sides take two runs.

The tests connect to `127.0.0.1:${TC_DB_PORT:-33306}`, and so does CI, so a non-default `TC_DB_PORT` has to be exported for both commands.

CI (`.github/workflows/ci.yml`) runs web (Biome / typecheck / Vitest / build) and API (core checks, codegen drift, conformance, upgrade path, build, vet, and E2E with and without object storage).

## Migrations and code generation

- The schema is layered. `sql/core/` is a copy of the shared schema contract that lets more than one product write to the same database; it is not edited here and is pinned upstream in `sql/core/UPSTREAM.json`. `sql/time/` holds this application's own tables.
- `sql/core/tools/build-schema.sh core time` concatenates core then time into `sql/schema.sql` (`make db-schema`), which `make db-apply` applies to an empty database.
- An existing database is upgraded rather than rebuilt; see [Upgrading](deployment.md#upgrading). `bash scripts/check-upgrade-path.sh` checks that the upgrades take a v0.1.0 database, with data, to the current schema.
- `make test-conformance` checks the resulting schema against the contract. `make check-core` checks offline that the vendored copy is unedited and that no product layer alters a core table; `make check-core-pin` also checks the copy against the commit it claims to come from, and needs network access to GitHub.
- Write queries in `sql/queries/*.sql` and run `make sqlc` to generate Go code into `apps/api/internal/db/generated/`.
