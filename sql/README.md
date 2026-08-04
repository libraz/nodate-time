# Schema layout

`tables/` holds this application's own schema, concatenated into
`schema.sql` by `build-schema.sh`. That is the schema the API runs on
today.

`core/` is something different: a vendored copy of the shared schema
contract, describing tables that more than one product may implement so
that two independently deployable applications pointed at the same
database can write each other's rows. It is a specification, not a
library — there is no build-time dependency in either direction, and
nothing in this repository imports it.

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

## Status

`core/` is vendored but not yet applied. This application's tables
predate the contract and differ from it in ways that need a migration
rather than a merge — most visibly, `events` here is the calendar-event
table, while in the contract `events` is the append-only log and
calendar rows live in `calendar_events`.
