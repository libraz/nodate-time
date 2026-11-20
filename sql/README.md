# Schema layout

The schema is composed from two layers by
`core/tools/build-schema.sh core time`, which writes the concatenation to
stdout; `schema.sql` is the committed result of that redirection and is
what the application runs on.

`core/` is a vendored copy of the shared schema contract: tables that
more than one product may implement, so that two independently
deployable applications pointed at the same database can write each
other's rows. It is a specification, not a library — there is no
build-time dependency in either direction, and nothing here imports it.

`time/` holds this application's own tables, the ones no other product
needs to agree about.

`core/PROTOCOL.md` states what an implementation has to honour, and
`core/conformance/` is a runnable suite that checks whether it does.

## The copy is not a source

`core/` is upstream's text, pinned in `core/UPSTREAM.json` by commit and
by a hash per file. Editing it here would not change what the other
product writes; it would only make this repository's description of the
shared schema disagree with the schema itself, and that disagreement
shows up as corrupt data rather than as a failed build.

To change the contract, change it upstream and re-vendor.
`bash sql/core/tools/check-vendored.sh sql/core --mode <mode>` verifies it:
`local` checks the file hashes offline, `pin` adds a diff against the pinned
commit, and `all` also checks that upstream has not moved on. CI runs `pin`
on every build and `all` on a daily schedule.

The time layer never changes the shape of a core table: it has no
`alters/`, and `sql/core/tools/check-no-core-alters.sh sql` fails if one of
its files alters or indexes a core table. A column this application needs
on a shared table goes into core upstream.

## A schema change is not finished until the generated code follows

`schema.sql` and the sqlc output under `apps/api/internal/db/generated/`
are both derived from the layered sources. Neither is edited by hand,
and neither is regenerated automatically.

Leaving the generated code behind is quiet. A column added to a table
but missing from the generated struct is not a compile error — the field
simply does not exist, so the build succeeds, the tests pass, and the
gap only surfaces when somebody tries to use the column and cannot.

Column comments count too: sqlc copies them into the generated Go doc
comments, so a comment-only edit still leaves the generated files stale.

So a change under `sql/` runs both generators before it is done:

```sh
make db-schema   # layered sources -> schema.sql, time/upgrades/triggers.sql
make sqlc        # schema + queries -> apps/api/internal/db/generated/
```

`make verify-codegen` checks the pairing without writing anything: it runs
`scripts/check-codegen-drift.sh`, compares `time/upgrades/triggers.sql` with
`scripts/gen-layer-triggers-upgrade.sh time`, and runs
`scripts/check-error-messages.sh`; CI runs it. The pre-commit hook (`.githooks/pre-commit`) runs
`check-codegen-drift.sh --staged`, plus
`sql/core/tools/check-vendored.sh sql/core --mode local` when `sql/core/` is
staged; it does not run the error-message check. The hook is
reached through `core.hooksPath`, so it only runs where that is configured.

## A schema change also ships its upgrade

`schema.sql` creates a database from nothing, dropping every table of its
layers first, so it is never run against one that holds data. An existing
database reaches the current schema through upgrades, applied in this order
with the writers of every product on the database stopped:

1. `core/upgrades/NNNN-<slug>.sql`, all of them in number order. They
   reshape the shared tables and are vendored from upstream with the rest
   of `core/`.
2. `time/upgrades/from-v0.1.0.sql`, only when its marker is absent: it has
   been applied iff `album_photos.retired_at` exists. It changes this
   layer's tables, rewrites the rows this application wrote, and refuses to
   run before the core upgrades.
3. The feature files, in this order: `time/upgrades/schedule-polls.sql`,
   `time/upgrades/calendar-covers.sql`.
4. `time/upgrades/triggers.sql`, last.
5. `bash sql/core/conformance/run.sh --mode schema` against the database.

A release file (`from-<release>.sql`) is the step from that tag. It is
applied once, never re-run after the new API has served, and frozen once a
database past it has served. A layer object added after that goes into a
feature file (`<feature>.sql`), listed in step 3: idempotent, written as
`CREATE TABLE IF NOT EXISTS` with a copy of the table file,
`CREATE OR REPLACE VIEW`, and information_schema-guarded steps. A change to
a core table is made upstream and arrives as a new core upgrade. When a
change alters what an existing row means, the file says how to tell the old
rows apart and sets them accordingly, the way `from-v0.1.0.sql` marks the
rows v0.1.0 had already deleted, and the fixture gains rows for it.

`time/upgrades/triggers.sql` is generated from `time/triggers/` by
`make db-schema` and is never edited by hand. It drops and recreates every
trigger under a fixed `sql_mode`. MySQL has no atomic trigger replace, so it
runs with the writers stopped. A trigger removed from `time/triggers/`
keeps its `DROP` through the generator's `RETIRED` list.

`bash scripts/check-upgrade-path.sh` proves it. It loads three starting
schemas: v0.1.0 and the one main carried before the core upgrades existed,
each with `scripts/fixtures/v0.1.0-data.sql`, and f3c4d681, a database
already past `from-v0.1.0.sql`, which skips it. It applies the upgrades
twice, checks the rewritten rows on the fixture states, runs the
conformance suite and compares `information_schema` against a fresh load of
the working tree's `schema.sql`.

## Queries

`queries/*.sql` is the input to sqlc alongside the composed schema. The
generated package is output, never edited.
