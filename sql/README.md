# Schema layout

The schema is composed from two layers by `build-schema.sh`, which
writes the concatenation to stdout; `schema.sql` is the committed result
of that redirection and is what the application runs on.

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
`scripts/check-vendored-core.sh` verifies both halves of that: that the
copy is unedited, and that it still matches the pinned ref.

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
make db-schema   # layered sources -> schema.sql
make sqlc        # schema + queries -> apps/api/internal/db/generated/
```

`make verify-codegen` checks the pairing without writing anything, and
is what the pre-commit hook and CI both run.

## Queries

`queries/*.sql` is the input to sqlc alongside the composed schema. The
generated package is output, never edited.
