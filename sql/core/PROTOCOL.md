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

| Path                       | Contents                                                           |
| -------------------------- | ------------------------------------------------------------------ |
| `tables/`                  | Every table shared across implementations.                         |
| `triggers/`                | The invariants that cannot be expressed as constraints.            |
| `conformance/`             | A runnable suite that checks an implementation.                    |
| `tools/`                   | Schema build, vendoring and layer checks, identical in every copy. |
| `upgrades/`                | Idempotent scripts that bring an existing core schema up to date.  |
| `recurrence/fixtures.json` | Recurrence rules with the occurrences every expander must produce. |
| `event-kinds.yaml`         | The registry of event kinds written for changes to core tables.    |

A product adds its own tables in a sibling layer — this repository keeps
its own under `sql/flow/`. Layers compose: a deployment applies core, then
each product layer it hosts. Adding a second product to an existing database
is additive DDL, never a migration.

## Layers

| Layer  | Contents                                                                                      |
| ------ | --------------------------------------------------------------------------------------------- |
| `core` | The shared tables, triggers, this document, conformance, tools, upgrades, fixtures, registry. |
| `time` | Tables and views only the calendar product uses, its upgrades and its event-kind registry.    |
| `flow` | Tables only the task product uses, its cross-layer `constraints/`, views and triggers.        |

Each layer has one source repository; every other repository that needs it
vendors a copy. A database is built from one of two sets:

| Set      | Layers           | MySQL            | Build                                                |
| -------- | ---------------- | ---------------- | ---------------------------------------------------- |
| time-set | core, time       | 8.4 LTS or later | `bash sql/core/tools/build-schema.sh core time`      |
| full-set | core, time, flow | 9.6              | `bash sql/core/tools/build-schema.sh core time flow` |

An existing time-set becomes a full-set with
`bash sql/core/tools/build-schema.sh flow`, which drops and recreates the
flow layer's objects only. The result has the same tables, columns,
indexes, constraints, views and triggers as a full-set built from empty.

`build-schema.sh` drops every table of the layers it is given before it
creates them. Pointing it at core or time on a database that holds data
destroys that data; see [Upgrading](#upgrading).

A product layer never changes the shape of a core table. Adding, modifying,
renaming or dropping a column, an index or a `CHECK` on a core table is not
allowed from a product layer, and a layer directory has no `alters/`. The
one exception is a foreign key that crosses layers, declared as described
below. `tools/check-no-core-alters.sh` enforces both halves.

## Layer boundaries

Core columns exist even when the layer that gives them meaning is absent, so
that every implementation writes rows of the same shape. `calendar_events.task_id`
and the `events` actor columns are the current examples: a deployment with no
task layer simply leaves them NULL.

Foreign keys, unlike columns, cannot point at a table that may not exist.
A key that crosses from core into a product layer is therefore declared by
that layer, as an `ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY` under its
`constraints/` directory, and runs after every layer's `CREATE TABLE`. That
clause is the only one a `constraints/` file may contain.

The practical rule when adding a column to core: it must be nullable or
carry a default. An implementation that has not adopted the change yet still
has to be able to insert.

## Obligations

An implementation that writes to a shared database must do all four.

### 1. Write `calendar_events` rows per the DDL

Shape is enforced by the table itself — `CHECK` constraints, foreign keys,
the `task_singleton_role` generated column, and the projection guard
triggers. An implementation does not need to re-derive these rules; it needs
to not work around them.

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

| Column              | Meaning                                                                                                                         |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `id`                | `BIGINT UNSIGNED AUTO_INCREMENT`. Monotonic, so a consumer tails with `WHERE id > last_seen`.                                   |
| `public_id`         | UUID v7, `BINARY(16)`. The only identifier that may appear in an API response.                                                  |
| `workspace_id`      | Scope. Required.                                                                                                                |
| `type`              | Dotted kind, e.g. `calendar.event.created`. Consumers match on prefix, so a new kind is additive.                               |
| `payload_json`      | The change. Unknown keys must be preserved by anything that round-trips a row.                                                  |
| `occurred_at`       | Logical time, millisecond precision. Ties break by `id`.                                                                        |
| `actor_user_id`     | Acting user, or NULL for a system action.                                                                                       |
| `calendar_id`       | The calendar the change is about, or NULL. Present so a feed for one calendar reads the log rather than a second history table. |
| `subject_public_id` | The `public_id` of the entity the kind names as its subject, or NULL. Written by the writer; see [Event kinds](#event-kinds).   |

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

| Variable                     | Lifts                                   |
| ---------------------------- | --------------------------------------- |
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

## Event kinds

Every `events.type` written for a change to a core table is listed in
`event-kinds.yaml`. A product layer lists the kinds it adds for its own
tables in `sql/<layer>/event-kinds.yaml`, in the same shape:

```yaml
kinds:
  - kind: calendar.event.created
    family: calendar
    subject: calendar_events
    required_payload_keys: [eventId, title]
```

- `kind` is the string written to `events.type`. A writer appends only
  kinds that a registry present in the deployment lists. A new kind goes
  into a registry before anything emits it.
- `family` is the dotted prefix a reader routes the kind by.
- `subject` names the table whose `public_id` the writer puts in
  `events.subject_public_id`. When it is empty the column stays NULL.
  Every `calendar.event.*` kind, including the comment, checklist,
  attachment, attendee, rsvp and invite kinds below it, has the event as
  its subject. `calendar.memo.*` has the memo. Calendar lifecycle,
  membership, subscription, reminder and `workspace.*` kinds have none.
- `required_payload_keys` are present in every row of that kind. A
  `calendar.event.*` payload always carries `eventId` and `title`, and a
  kind about an entity below the event also carries that entity's id.
  Writers may add keys; anything that round-trips a row keeps them.

A concept both products have uses one name. Membership changes use the
`added` / `removed` / `role_changed` / `updated` vocabulary, and a member
who leaves on their own is `calendar.member.removed` with `reason: "left"`.
An answer to an invitation is `calendar.event.rsvp.updated`; a change to
what an attendee may do is `calendar.event.attendee.updated`.

A reader skips a kind it does not recognise rather than failing on it, so a
registry can grow without every consumer moving at once.

Tools read only the `- kind:` lines of a registry, so a registry is parsed
without a YAML library. The conformance data mode checks that every
`calendar.*` and `workspace.*` row in `events` carries a kind listed in the
union of the registries present.

## Recurrence

`calendar_events.recurrence_rule` is either NULL (a single occurrence) or a
JSON object in this canonical shape:

```json
{ "freq": "weekly", "interval": 2, "byDay": ["TU", "SU"], "wkst": "SU", "count": 10 }
```

| Key          | Type and range                                              | Meaning                                                                              |
| ------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `freq`       | Required. `daily`, `weekly`, `monthly` or `yearly`.         |                                                                                      |
| `interval`   | Optional. Integer 1..999.                                   | Absent means 1.                                                                      |
| `byDay`      | Optional. Array of `MO`..`SU`, 1..7 items, no repeats.      | Weekly: the set of weekdays. Monthly: one weekday, paired with `bySetPos`.           |
| `byMonthDay` | Optional. Array of integers 1..31, 1..31 items, no repeats. | Monthly only. A day the month does not have is clamped to the month's last day.      |
| `bySetPos`   | Optional. One-item array of 1..5 or -1.                     | Only together with `byDay`. Applied after the other `by*` keys.                      |
| `wkst`       | Optional. `MO` or `SU`.                                     | Absent means `MO`. Changes the result only for weekly rules with `interval` above 1. |
| `until`      | Optional. An RFC 3339 instant, or a `YYYY-MM-DD` date.      | Inclusive. A date means the end of that day in the event's time zone.                |
| `count`      | Optional. Integer 1..1000.                                  | Number of occurrences, the first included. Not combined with `until`.                |

No other key is allowed, and no key may hold `null`. Every array form is an
array even with one item.

Expansion rules shared by every expander:

- The event's start is the first occurrence.
- Each occurrence keeps the start's wall-clock time in the event's time
  zone, so its UTC offset follows daylight-saving changes.
- Clamping is computed from the start for each occurrence, so a series on
  the 31st reads Jan 31, Feb 28, Mar 31 rather than staying on the 28th.
  A yearly series from Feb 29 falls on Feb 28 in other years.
- A `bySetPos` position the month does not have (a fifth Thursday) produces
  no occurrence that month.

The table enforces the shape with `CHECK chk_calendar_events_recurrence_shape`,
a `JSON_SCHEMA_VALID` schema written only with draft-4 keywords, the
draft MySQL enforces. The combinations that depend on `freq` — `byMonthDay`
only on monthly rules, `byDay` with `bySetPos` only on monthly rules — are
not in the `CHECK`; each implementation's validation refuses them before
the write.

`recurrence/fixtures.json` holds rules with the occurrences they must
produce, as `{name, rule, dtstart, tz, expect}`. A rule without `count` or
`until` is unbounded, and `expect` lists its first occurrences. Every
expander in every implementation runs these fixtures. Each fixture's
expectation comes from the rules above, not from any implementation's
output.

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
carrying a `shared` label _and_ an owner still vanishes when that one person
is removed. A calendar a group shares leaves `owner_user_id` NULL; that is
the whole difference, and it is the difference that has teeth.

Resolve the calendar id and check the grant in the same function. Splitting
them is how an authorization check goes missing — not because someone
decides to skip it, but because the two steps can be performed apart.

## Identity

Users, identities and sessions are shared rows. Each product keeps its own
sign-in implementation, and every implementation follows these rules.

1. **Every path that issues a session checks the same things.** Password,
   OAuth/OIDC, magic link, completing a password reset, signing in after
   email verification, and refresh all confirm that the user and the
   identity are enabled and not locked. When the local identity has
   `mfa_confirmed_at` set, the path verifies a second factor; a path that
   does not implement one refuses that user.
2. **Linking is keyed by (provider, subject).** A provider identity may be
   attached to an existing user by email address only when the provider
   asserts the address is verified and the user either has
   `email_verified_at` set or has neither a password nor MFA. Anything else
   is refused. Linking never clears or rewrites `password_hash`, the MFA
   secret or recovery codes.
3. **Sign-in restriction.** Sign-in is restricted when the deployment's
   configured domain list is non-empty or `oauth_signin_allowlist` has at
   least one enabled row. While restricted, a verified address is admitted
   only if it matches a configured domain, a `domain` row or an `email`
   row.
4. **Lockout.** `identities.failed_attempts` and `locked_until_at` are
   shared, and each implementation honours values the other wrote. Five
   consecutive failures lock the identity for 15 minutes; on the recovery
   code path the limit is three.
5. **Sessions.** Each implementation writes its own name to
   `sessions.audience` as a SQL literal (`'flow'`, `'time'`) and refreshes,
   looks up and physically deletes expired rows of its own audience only.
   Rotation and reuse detection are per implementation. Revoking all of a
   user's sessions — on a password change or reset, an administrator
   disabling the user, or a link — revokes every row regardless of
   audience. No row has an empty audience.
6. **Email addresses** are lower-cased before they are stored and before
   they are compared. `email_verified_at` is set only when control of the
   address has been proven.
7. **Password hashes** are argon2id PHC strings, verifiable with either
   implementation's parameters.
8. **Workspace membership is a precondition.** Calendar access checks the
   enabled `workspace_members` row of the calendar's workspace in addition
   to `calendar_members`. A disabled membership is revived only by an
   explicit addition — an administrator action or a workspace invitation —
   never by automatic joining or by accepting a join link. Every enabled
   `calendar_members` row has an enabled `workspace_members` row for that
   calendar's workspace.
9. **Adding a workspace member** writes, in one transaction, the
   `workspace_members` row, the member's personal calendar and its
   subscription, and the `workspace.member.added` event. The first user to
   join a workspace with no owner becomes its owner.

The conformance data mode checks the rules a row can show: audience is
never empty, `users.email` has no upper-case letters, and calendar grants
sit inside workspace membership.

## Storage

`storage_objects` rows and the blobs they name are shared by every layer.

- **Reference count.** `storage_objects.ref_count` equals the number of rows,
  in any layer and in any state — reserved, live or retired — that point at
  the object by foreign key. Inserting such a row adds one in the same
  transaction; deleting it subtracts one in the same transaction. Soft
  deletion and retirement leave the count alone. Where a foreign key is
  cleared by `ON DELETE SET NULL` (`users.avatar_storage_object_id`), the
  writer that clears it subtracts one.
- **Storage key.** `storage_key` is chosen when the row is created and is
  never rewritten. Confirming an upload updates only `uploaded_at` and the
  verified `byte_size` and `content_type`.
- **Reclaiming.** There are exactly two sweeps. The unconfirmed sweep takes
  objects with `uploaded_at IS NULL` and `updated_at` older than the cutoff,
  deletes the referencing rows in tables the sweeper's layer owns, and
  deletes the object in the same transaction. The unreferenced sweep takes
  confirmed objects (`uploaded_at IS NOT NULL`) with `ref_count = 0`, with no
  age condition. A reservation is never taken by it, however old: only the
  unconfirmed sweep reclaims one.
- **Time limits.** An upload URL for a reservation expires within 15 minutes
  of the row's `updated_at`. A sweeper's cutoff is at least 45 minutes, so
  one implementation's sweep never removes another's upload in progress.
- **Foreign rows.** A delete refused by a `RESTRICT` foreign key means the
  object is still referenced from a layer the sweeper does not own. The
  sweeper skips that object and continues; it does not fail the pass.
- **Blob removal.** Blobs are removed only through `storage_blob_removals`.
  The transaction that deletes the rows enqueues the key. A drainer removes
  the blob first and the queue row second, and treats removing a missing
  key as success, so drainers from both products can run side by side.
- **Bucket identity.** On start-up each drainer compares its configured
  location, `<endpoint>/<bucket>`, with the `instance_settings` key
  `storage.location`. It inserts the row when it is missing. On a mismatch
  it stops the drainer and records an error; the application keeps
  running.

The conformance data mode collects every foreign key that references
`storage_objects(id)`, counts the referencing rows per object, and checks
the count against `ref_count`.

## Realtime across processes

Two products on one database each have their own in-process fan-out, which
does not reach the other. The `events` table closes that gap: its primary
key is monotonic, so either side can tail it with `WHERE id > last_seen` and
feed its own notifier. No broker and no shared code is involved.

One detail decides whether a tailer is correct. `AUTO_INCREMENT` assigns an
id when the `INSERT` runs, but the row stays invisible until its transaction
commits — so a row can appear _below_ an id that has already been read. A
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

It runs in four modes, chosen with `--mode`:

- **schema** (default) — applies assertions against structure and guard
  behaviour on a database with the core schema loaded. Safe on an empty
  database; it creates and removes its own fixtures.
- **data** — sweeps an existing database for rows that violate the
  obligations, which is how a writer that skipped the event log or bypassed
  the guard is caught after the fact.
- **all** — runs schema and then data.
- **selftest** — plants one violation per check and per `CHECK` keyword,
  confirms each is refused, and cleans up, so a check that has stopped
  detecting anything fails instead of passing silently.

Both products run the schema mode in CI: this one to prove the reference
still matches what it documents, an implementor to prove it conforms.

## Upgrading

A new database is built by running `tools/build-schema.sh` on an empty
database. A database that already holds data is never rebuilt: running
`build-schema.sh` for core or time on it drops their tables and the data
with them.

An existing database is brought up to date in this order:

1. Stop every application that writes to it.
2. Apply `sql/core/upgrades/*.sql` in numeric order.
3. Apply each hosted product layer's upgrades.
4. Run `bash sql/core/conformance/run.sh --mode schema`.
5. Start the applications.

Core upgrades are named `NNNN-<slug>.sql`. Each one is idempotent: it
inspects `information_schema` inside a stored procedure and acts only on
what is still missing or different, comparing attributes such as
`COLUMN_TYPE`, `CHARACTER_SET_NAME` and `GENERATION_EXPRESSION` rather than
mere existence. Applying all of them in order from any earlier core state
yields the current schema, and applying them twice changes nothing. A
shipped upgrade is never edited; a later change is a new file.

An upgrade that rewrites data first makes the changes that do not alter
meaning, then lists by `public_id` any rows that still violate the new
constraint and stops, before it adds the constraint.

A product layer's upgrades live in `sql/<layer>/upgrades/`, one file per
release they upgrade from. A layer that does not promise to preserve data
ships none and is rebuilt from its DDL instead; the flow layer is rebuilt
this way until its first stable release.

## Deployment

- **MySQL.** The core and time layers work on 8.4 LTS and on 9.6. The flow
  layer needs 9.6.
- **One database, one bucket.** In a full-set, every application uses the
  same database, the same object-storage bucket and the same sign-in
  domain list. An application that bootstraps its workspace by slug is
  configured with the slug of the existing workspace.
- **Reminders.** Server-side reminder delivery is done by the flow layer's
  scheduler alone. Other implementations export reminders as `VALARM` and
  do not deliver them.
- **Versions move together.** New constraints, columns and rules can refuse
  writes from an older application, and running an older application
  against an upgraded database is not supported. Upgrades are applied with
  the applications stopped.

## Versioning

This repository hosts the reference copy. An implementor vendors the
directory with `tools/vendor.sh`, which pins it to a commit sha and records
each file's hash in `UPSTREAM.json`. `tools/check-vendored.sh` runs in every
build and fails when the vendored copy was edited or differs from that pin,
so divergence fails a build instead of surfacing as corrupt data months
later. A scheduled job reports when the upstream has moved on without a
re-vendor.
