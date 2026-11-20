# Contributing to Nodate Time

Small and unfinished changes are welcome, including a bug report with no fix. You do not need to satisfy or read all of this document. The maintainer reviews every pull request, finishes what is missing and merges it.

## What a pull request needs

- A description of what the change does and why. A clear intent is enough for the maintainer to verify the behaviour.
- If possible, a build and the tests for the part you changed.

A red check, a missing translation or test, or an untouched part of the stack is fine. Pull requests target `main`. Opening an issue first is optional and only worth it for a larger change.

### What the maintainer can verify

The maintainer tests on macOS in English and Japanese, and on Windows in Japanese only. For a bug that depends on Windows in another language (keyboard layout, IME, system font, locale-specific shortcut), describe what you see, ideally with a screenshot, because it cannot be reproduced.

## What you are contributing to

Nodate Time is a Go API (`apps/api`) and a React single-page app (`apps/web`) on MySQL. The `sql/core/` directory is a vendored copy of a schema contract shared with [nodate-flow](https://github.com/libraz/nodate-flow); schema changes to the shared tables belong there, not here (see [Changing the shared schema](#changing-the-shared-schema)).

## Development setup

Requirements: [Bun](https://bun.sh), Node.js and Go at the versions in `mise.toml` and `package.json`, Docker (MySQL, object storage and mail catcher), a host `mysql` client (used by `make test-conformance` and `make test-upgrade-path`) and [sqlc](https://sqlc.dev).

```sh
bun install
git config core.hooksPath .githooks   # pre-commit: blocks edits to sql/core
cp .env.example .env                  # optional; the defaults work locally
make db-up
make db-seed                          # schema and sample data
make storage-up
make dev                              # MySQL + mailpit + API + web
```

The web app is on <http://localhost:5173> and the API on <http://localhost:8080>. See [Development](https://github.com/libraz/nodate-time/blob/main/docs/en/development.md) for the layout and [Configuration](https://github.com/libraz/nodate-time/blob/main/docs/en/configuration.md) for every variable.

## Checks

| Check                                                                                | Workflow             | Trigger                      |
| ------------------------------------------------------------------------------------ | -------------------- | ---------------------------- |
| Biome, typecheck, Vitest, production build (`web`)                                   | `ci.yml`             | push to `main`, pull request |
| Vendored contract check, generated code, conformance, upgrade path, vet, E2E (`api`) | `ci.yml`             | push to `main`, pull request |
| Vendored copy against the current upstream (`make check-core-current`)               | `vendored-drift.yml` | daily, manual                |

A red check is no reason to hold a pull request back.

## Checklist

A merged change looks like this. Anything missing, the maintainer fills in.

- Run `bun run check` and `bun run typecheck` for web changes.
- Run `make verify-codegen` after touching `sql/`, and commit the regenerated `sql/schema.sql` and `apps/api/internal/db/generated/` in the same change (`make db-schema`, `make sqlc`).
- Add an E2E test for API behaviour under `apps/api/tests/e2e/`, with its own tenant (`helpers.NewTenant`); do not use `PurgeAll`.
- Give optional request fields `required:"false"`; Huma treats every field as required otherwise.
- Resolve calendars through the `resolveCalendar` helpers so the membership and role check comes with the lookup.
- Declare API errors as `Spec` values in `apps/api/internal/errors/`.
- Keep user-facing text in `apps/web/src/i18n/` for both English and Japanese.
- Write code, comments and commit messages in English. Commit messages use a Conventional Commits prefix (`feat(web): ...`); the maintainer can rewrite one at merge.

## Reference

This section asks nothing; it explains how the pieces behave.

### Verification commands

```sh
bun run check                         # Biome
bun run typecheck                     # tsc
make verify-codegen                   # schema, trigger upgrade and sqlc output match their sources
make test-api                         # Go unit tests
make db-up && make test-e2e           # E2E with TC_TEST_INTEGRATION=1
make test-e2e-storage                 # adds TC_TEST_STORAGE=1; needs `make storage-up`
make test-e2e-storage-absent          # the 503 answers when object storage is absent
make test-conformance                 # schema against the shared contract
make test-upgrade-path                # upgrades from older databases
```

A plain `go test ./...` passes with the object-storage tests skipped, so read the skip count rather than the exit status. The storage-absent tests skip whenever `TC_TEST_STORAGE` is set, and the test server is built once per process, so a complete E2E check is two runs: `make test-e2e-storage` and `make test-e2e-storage-absent`. The suite connects to MySQL on `127.0.0.1:${TC_DB_PORT:-33306}`.

`sql/schema.sql` drops every table when applied. To add a column to a test database that other tests are using, apply an `ALTER` directly instead.

### Changing the shared schema

`sql/core/` is not edited in this repository. The pre-commit hook and `make check-core` reject an edit, and `sql/core/UPSTREAM.json` records the upstream repository, the commit the copy came from and a hash of every file.

1. Open a pull request against [libraz/nodate-flow](https://github.com/libraz/nodate-flow) that changes `sql/core`.
2. After it is merged, re-vendor from a checkout of that repository (or its clone URL), here:

   ```sh
   bash sql/core/tools/vendor.sh --from /path/to/nodate-flow --dest sql/core --ref <commit>
   ```

   `--ref` defaults to `HEAD` and is pinned as a full commit sha; `--src` defaults to `sql/core`. The script replaces the destination wholesale and rewrites `UPSTREAM.json`. Only committed content is taken.
3. Run `make db-schema sqlc verify-codegen test-conformance test-upgrade-path` and commit the result together with any change in `sql/time/` that depends on it.

To try a change before it is merged upstream, vendor from a fork: `--from https://github.com/<you>/nodate-flow.git --dest sql/core --ref <commit>`, or from a local checkout with the commit made. `UPSTREAM.json` then records that fork, so `make check-core-pin` and CI do not accept it; use it for local testing and re-vendor from the upstream repository before the pull request is merged.

`sql/time/` must not change the shape of a core table. A column or index on a core table goes into the shared contract first.

Check targets: `make check-core` is offline and verifies the hashes and that no product layer alters a core table. `make check-core-pin` also compares the copy with its recorded upstream commit and needs network access to GitHub. `make check-core-current` compares it with the upstream default branch.

### Existing databases

An existing database is upgraded, not rebuilt. The order is in `sql/README.md`; `bash scripts/check-upgrade-path.sh` checks it from three starting states on scratch databases.

## Reporting issues

Include the version or commit, the OS and its display language, the browser (for the web app), and what you expected. For a calendar import problem, attach the file if it can be shared. Report security problems as described in [SECURITY.md](SECURITY.md), not in a public issue.

## License

Contributions are licensed under [AGPL-3.0](LICENSE).
