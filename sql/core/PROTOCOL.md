# The nodate core schema

`sql/core/` is a specification, not a library. It describes a set of tables,
constraints and triggers that more than one product may implement, so that
two independently deployable applications pointed at the same database can
write each other's rows without sharing a line of code.

There is no build-time dependency in either direction. An implementor copies
the DDL in this directory, applies it, and honours the obligations below. If
it does that, the two products interoperate; if it does not, the database
refuses the write or the reconciler reports the drift. Nothing relies on
either side reading the other's source.

## What is in here

| Directory | Contents |
| --- | --- |
| `tables/` | Every table shared across implementations. |
| `triggers/` | The invariants that cannot be expressed as constraints. |
| `conformance/` | A runnable suite that checks an implementation. |

A product adds its own tables in a sibling layer — this repository keeps
its own under `sql/flow/`. Layers compose: a deployment applies core, then
each product layer it hosts. Adding a second product to an existing database
is additive DDL, never a migration.

## Layer boundaries

Core columns exist even when the layer that gives them meaning is absent, so
that every implementation writes rows of the same shape. `calendar_events.task_id`
and the `events` actor columns are the current examples: a deployment with no
task layer simply leaves them NULL.

Foreign keys, unlike columns, cannot point at a table that may not exist.
A key that crosses from core into a product layer is therefore declared by
that layer, as an `ALTER TABLE` under its `constraints/` directory, and runs
after every layer's `CREATE TABLE`.

The practical rule when adding a column to core: it must be nullable or
carry a default. An implementation that has not adopted the change yet still
has to be able to insert.

## Obligations

An implementation that writes to a shared database must do all four.

### 1. Write `calendar_events` rows per the DDL

Shape is enforced by the table itself — `CHECK` constraints, foreign keys,
the `task_role_key` generated column, and the projection guard triggers. An
implementation does not need to re-derive these rules; it needs to not work
around them.

A recurring series is one row carrying the rule, and there are exactly two
ways to depart from it. Cancelling a single occurrence appends its start to
`recurrence_exceptions`. Changing one produces a second row naming the
series in `recurrence_parent_id` and the occurrence it replaces in
`recurrence_original_start`; that row carries no rule of its own.

Neither substitutes for the other, and an implementation must not add a
third. A cancellation expressed as a row, or an override that also claims
to be cancelled, gives a consumer two places to look before it can say
whether an occurrence happens — and the two answers drift apart the first
time a writer updates one and not the other.

`owner_user_id` is required, and it decides whose layer and colour the
event appears on. There is no such thing as an event belonging to the
calendar rather than to a person: a writer importing from somewhere that
has no notion of an owner files the event under the acting user, which is
the honest answer — that user is who put it there.

One rule the schema cannot enforce for itself: `show_as` and `flexibility`
are separate axes and must stay that way. `show_as` is iCalendar `TRANSP` —
whether the time reads as taken — and is what every external free/busy
consumer reads. `flexibility` is whether the commitment could move. A
meeting its owner would gladly reschedule and one that cannot move are both
`busy`, which is why a second column exists.

Writing `tentative` to mean "negotiable" is the tempting shortcut and the
one that does real damage: it puts a non-iCalendar meaning into the column
outside consumers interpret, so a free/busy export starts misreporting to
people who never agreed to this contract. Display may collapse the two into
a single mark; storage may not.

### 2. Append to `events` in the same transaction as the state change

Every state change gets exactly one row in `events`, written in the same
transaction as the rows it describes. A change that lands without its event
is invisible to every other process on the database: notification fan-out,
realtime delivery, and audit all read the log rather than polling tables.

Column semantics:

| Column | Meaning |
| --- | --- |
| `id` | `BIGINT UNSIGNED AUTO_INCREMENT`. Monotonic, so a consumer tails with `WHERE id > last_seen`. |
| `public_id` | UUID v7, `BINARY(16)`. The only identifier that may appear in an API response. |
| `workspace_id` | Scope. Required. |
| `type` | Dotted kind, e.g. `calendar.event.created`. Consumers match on prefix, so a new kind is additive. |
| `payload_json` | The change. Unknown keys must be preserved by anything that round-trips a row. |
| `occurred_at` | Logical time, millisecond precision. Ties break by `id`. |
| `actor_user_id` | Acting user, or NULL for a system action. |
| `calendar_id` | The calendar the change is about, or NULL. Present so a feed for one calendar reads the log rather than a second history table. |

The table is append-only. Corrections are new rows, never an `UPDATE` or
`DELETE` of an existing one.

### 3. Do not mutate rows whose `task_id` is non-NULL

Such a row is a projection of a task in another layer. Its `task_id`,
`task_role`, `title`, `start_at`, `end_at` and `enabled` mirror that task,
and changing one of them without moving the task desyncs the pair silently.

Columns with no counterpart in the projecting layer — `memo`, `location`,
`visibility`, `show_as`, `url`, `block_label` — stay freely writable on a
projected row. The guard draws the line at the mirrored columns only.

An implementation that owns no task layer never encounters this: its
`task_id` is always NULL and none of the guard's branches can fire.

### 4. Follow the session-variable protocol for guarded writes

A guard that could not be lifted would make the owning layer unable to write
its own rows. Each guard therefore has an opt-in session variable, and the
component that legitimately owns those rows sets it around its write.

| Variable | Lifts |
| --- | --- |
| `@nf_item_projection_engine` | The `calendar_events` projection guard. |

Three rules make the opt-in safe:

- Set it on the connection that performs the write. It is session-scoped, so
  issuing it through a pool can land the `SET` and the write on different
  connections.
- Clear it before the connection goes back to the pool, and before the
  transaction commits or rolls back. A connection returned with the guard
  down hands an unrelated request a database with no invariants.
- Set it only around a write that genuinely owns the invariant. Using it to
  silence a rejection reintroduces exactly the drift the guard prevents.

## Access

Two tables answer two different questions, and an implementation must not
collapse them.

`calendar_members` is the access grant: a row says a user may reach a
calendar, and its `role` says what they may do — `owner` > `manager` >
`editor` > `viewer`. Every read and every write must resolve through it.

`calendar_subscriptions` is one user's display preference for a calendar
they can already reach: their own colour override, and whether the layer is
toggled on. It grants nothing, its absence denies nothing, and treating it
as an ACL means anyone who once changed a sidebar colour keeps access they
were never given.

Workspace membership is a prerequisite for both, not a substitute: a
workspace may hold calendars whose audiences do not coincide.

There is no `shared` calendar kind, and an implementation must not add one.
`calendars.kind` says where the contents come from — written by people, or
pulled from a provider feed — and how many people can reach a calendar is
already a count of `calendar_members` rows. Encoding the same fact twice
gives the two encodings a way to disagree, and the disagreement is not
cosmetic: `calendars.owner_user_id` cascades on delete, so a calendar
carrying a `shared` label *and* an owner still vanishes when that one person
is removed. A calendar a group shares leaves `owner_user_id` NULL; that is
the whole difference, and it is the difference that has teeth.

Resolve the calendar id and check the grant in the same function. Splitting
them is how an authorization check goes missing — not because someone
decides to skip it, but because the two steps can be performed apart.

## Realtime across processes

Two products on one database each have their own in-process fan-out, which
does not reach the other. The `events` table closes that gap: its primary
key is monotonic, so either side can tail it with `WHERE id > last_seen` and
feed its own notifier. No broker and no shared code is involved.

One detail decides whether a tailer is correct. `AUTO_INCREMENT` assigns an
id when the `INSERT` runs, but the row stays invisible until its transaction
commits — so a row can appear *below* an id that has already been read. A
cursor that jumps straight to the highest id it has seen steps over that row
and never delivers it. A tailer must therefore keep re-reading from below
its high-water mark until a row has had time to commit, and suppress the
repeats with a set of ids it has already delivered. This bounds the
guarantee honestly: an append whose transaction stays open longer than that
window can still be missed, which is why obligation 2 asks for the event
row to be written in the same transaction as the change rather than in one
held open around unrelated work.

Delivery is at-least-once. A consumer must treat a repeat as harmless —
which is why the wire format carries no payload, only "something here
changed, re-read it".

## Conformance

`conformance/` holds a suite that checks an implementation against this
document. It is plain SQL driven by a shell runner, so it does not commit an
implementor to a language.

```sh
bash sql/core/conformance/run.sh --dsn 'user:pass@host:port/dbname'
```

It runs in two modes:

- **schema** (default) — applies assertions against structure and guard
  behaviour on a database with the core schema loaded. Safe on an empty
  database; it creates and removes its own fixtures.
- **data** — sweeps an existing database for rows that violate the
  obligations, which is how a writer that skipped the event log or bypassed
  the guard is caught after the fact.

Both products run the schema mode in CI: this one to prove the reference
still matches what it documents, an implementor to prove it conforms.

## Versioning

This repository hosts the reference copy. An implementor vendors the
directory, pins it to a tag, and runs a job that diffs the vendored copy
against that tag, so divergence fails a build instead of surfacing as
corrupt data months later.
