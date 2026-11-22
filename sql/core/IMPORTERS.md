# Importers

An importer is an operator tool that reads another service and writes the
rows it finds into a shared database. More than one product ships one, and
each is written independently: nothing here is code, and no importer reads
another's source. What they share is this document. An operator who has
used one importer can read the mapping, the report and the exit code of any
other, and two importers pointed at the same database do not disturb each
other's rows.

The fixtures under `importers/` turn the parts of this document that can be
checked mechanically into data. Every implementation runs all of them.

## Terms

| Term        | Meaning                                                                                                                |
| ----------- | ---------------------------------------------------------------------------------------------------------------------- |
| Container   | The row the imported entities live in: a calendar in time, a project in flow. Reports write it `calendar` / `project`. |
| Entity      | A kind of row tracked inside a container: events in time; tasks, comments, attachments and dependencies in flow.       |
| Main entity | The entity the deletion guard protects: events in time, tasks in flow.                                                 |
| Source      | The service imported from. Reports write its name as `TimeTree` / `Asana`.                                             |
| Source user | A person as the source identifies them, by the source's own id.                                                        |
| Importer    | The account the operator runs the tool as, named by the mapping's `importer` key.                                      |

## Command

```sh
import<source> -mapping <path|-> [-dry-run] [-sync]
```

- `-mapping` names the mapping file, or `-` for standard input.
- `-dry-run` reads everything, plans everything, prints the plan and writes
  nothing.
- `-sync` makes the database follow the source, deletions included; see
  [Sync](#sync).
- Credentials for the source come only from environment variables named
  `<SOURCE>_*`, never from a flag, so they stay out of shell history and
  process listings.
- The database comes from the product's own DSN configuration.
- A test points the tool at a fake source through a function option. There
  is no flag or environment variable for it.
- The report goes to standard output. A fatal error is one line on standard
  error, `error: <msg>`.

## Mapping

The mapping is a YAML document. A key the implementation does not know is
refused, and so is an empty document.

### Shared keys

| Key                         | Meaning                                                                                                                                                                                                     |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `importer`                  | Required. Email of the account the import runs as. It owns every container the import creates, and it stands in for any author the import cannot resolve.                                                   |
| `users[{email, name}]`      | Accounts to create. An account that already exists is left unchanged. An existing account that is not a member of the workspace is a resolution error.                                                      |
| `source_users{<id>: email}` | Source user id to account. The id is a string of decimal digits. An implementation whose source returns email addresses may match by address as well; an explicit entry wins. A dry-run prints every match. |
| containers                  | `calendars` in time, `projects` in flow. A list; each element is described below.                                                                                                                           |

A `source_users` value may be any account that exists or that `users`
creates. A person who is not a member of a given container counts as
unresolved in that container.

Each container element has:

| Key                      | Meaning                                                                                                                                                                                                                        |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `source`                 | Required. The source container's id, or a name that matches exactly one container. A value matching both an id and a name, or more than one container, is an error. A YAML integer is read as text. Time's id is `alias_code`. |
| `name`                   | Optional. Defaults to the source's name. The resolved names are unique within the mapping.                                                                                                                                     |
| `members[{email, role}]` | Members to add. Each product defines its role vocabulary; `editor` is in both. The importer is not listed.                                                                                                                     |

Any other key on a container element is the product's own.

### Validation rules

- An email address is an ASCII address and is compared lower-cased.
- `users`, each container's `members`, the containers' `source` and the
  containers' `name` hold no duplicates.
- `members` does not list the importer.
- A per-container owner setting (time's `default_owner`,
  `default_attendees` and the values of `labels`) names a member or the
  importer.
- A run needs at least one container. A dry-run may have none.

### Error paths

Every error reads `<path>: <detail>`. When the path is empty the line is the
detail alone.

- The path is the YAML location of the offending value. A map key is
  written `source_users["123"]`.
- An invalid field is reported at that field's path. A duplicate is
  reported at the identifying field of the second element that repeats it.
- The paths and the phrases each detail contains are listed under
  [Mapping rules](#mapping-rules).

## Validation before any write

Validation collects every error, in two stages, before it reports any:

1. The mapping on its own, by the rules in [Mapping rules](#mapping-rules).
   A mapping that does not parse — an empty document, or one with an unknown
   key — is reported as that single error; the remaining rules need a parsed
   mapping and do not run.
2. Resolution against the source and the database. Each of these errors also
   begins with the path of the mapping value it concerns. An email address
   referred to from more than one place takes the path of its first
   appearance. The detail is the product's own.

If there is any error, the tool prints `validation errors: N` and the
errors, exits 2, and has written nothing. Until then it may only fetch from
the source and read the database. A foundation row such as the workspace is
created, if it has to be, in the write stage, never during validation.

## Source tracking

- An imported row carries its source id in a `source_uid` column.
- A generated column `source_uid_key = IF(enabled, source_uid, NULL)`
  carries a `UNIQUE` key scoped to the row's parent, the container or the
  parent entity. A soft-deleted row therefore never blocks a re-import of
  the same id.
- Each product picks the column's type and collation.
- An implementation must tell the rows it wrote from rows another writer
  put in the same column. Each product picks how:

| Product | How it recognises its own rows                               |
| ------- | ------------------------------------------------------------ |
| time    | The `source` key in the payload of the row's creation event. |
| flow    | The `source_uid` value is namespaced: `asana:<gid>`.         |

A row with a `source_uid` that is not the implementation's own is
`orphaned`. It is counted, and never updated or deleted.

## Sync

- The source wins. For every mapped field the implementation compares the
  row's current value with the converted source value and overwrites the row
  when they differ, so a local edit to a mapped field is lost. A field the
  mapping does not cover is never touched.
- Something that disappeared from the source is soft-deleted through the
  same path the application uses for a deletion.
- When the source can answer whether a single item exists, the
  implementation may ask it before deleting. An item whose check returns a
  permission error, a server error or a timeout is not deleted; it is
  counted as `kept`.
- Child entities, comments for example, follow the same rules.
- A run without `-sync` never changes or deletes a row it imported before.
  It creates only the missing rows and the missing children.

## Deletion guard

The guard runs per container and per main entity, before anything is
written, and on a dry-run as well.

| Value     | Meaning                                                                                                                                                                   |
| --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `present` | Items the source listed for this container.                                                                                                                               |
| `synced`  | This implementation's live rows in the container.                                                                                                                         |
| `delete`  | Deletion candidates: rows in `synced` whose id is absent from this container's source list. Counted before any existence check, even by an implementation that makes one. |
| `moved`   | Candidates that moved to another mapped container.                                                                                                                        |
| `lost`    | `delete − moved`.                                                                                                                                                         |

The rules are checked in this order:

1. `present == 0 && synced > 0 && lost > 0` refuses with `empty-source`.
2. Otherwise `lost > 10 && lost*2 > synced` refuses with `mass-delete`.
3. Otherwise the container passes.

A refusal prints a `refusing:` line and the tool exits 2. Existence checks
run only for containers that passed, and they run on a dry-run too, so the
plan a dry-run prints is the plan a run would carry out.

`importers/sync-guard.json` holds the cases every implementation's guard
must decide the same way.

## Exit codes

| Code | Meaning                                                                                                                                     |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | Success.                                                                                                                                    |
| 1    | At least one write failed (`failed > 0`). The writes before and after it stay.                                                              |
| 2    | Nothing was written: bad input, missing credentials, a validation error, any fetch error, a guard refusal, or the lock held by another run. |

Every fetch error exits 2, whatever stage it happens in. An implementation
about to exit 0 after an error exits 2 instead. `skipped` never affects the
exit code.

## Report lines

Report lines are ASCII and English. `<name>` is formatted like Go's `%q`,
`<container>` is `calendar` or `project`, and `<Source>` is the source's
name.

### Shared lines

The shared lines are these, in the order they are printed:

```
<container> "<name>": sync plan <entity> create=N update=N unchanged=N delete=N moved=N kept=N synced=N orphaned=N repeated=N
sign-in restriction: open
sign-in restriction: domains=<n> allowlist=<n>
users to create: a@x, b@y
users to create: none
validation errors: N
  - <msg>
refusing: <container> "<name>" would delete N of M synced <entity>
refusing: <Source> returned no <entity> for <container> "<name>" but N are synced
<container> "<name>" (created|reused): members added=N existing=N; <entity> imported=N updated=N duplicates=N deleted=N kept=N moved=N skipped=N failed=N rejected=N[; <entity> ...]
total: <entity> imported=N updated=N duplicates=N deleted=N kept=N moved=N skipped=N failed=N rejected=N[; <entity> ...]; <container>s_created=N users_created=N
```

- `sync plan` lines appear on a dry-run with `-sync`, one per container and
  entity.
- `sign-in restriction:` and `users to create:` appear on a dry-run only.
  The restricted form always prints both counts; see
  [Account creation](#account-creation). `users to create:` lists addresses
  separated by a comma and a space, or `none`.
- `validation errors: N` appears on every dry-run, and on a run only when N
  is above zero. One `  - <msg>` line per error follows it.
- The first `refusing:` form is the `mass-delete` refusal and the second the
  `empty-source` one. In both, N is `lost` and M is `synced`.
- On a run, each container gets one line listing its entities in an order
  the product fixes, and the `total:` line comes last.

A product may print lines of its own before and between these; the shared
lines keep their relative order.

### Counters

On a run, each source item counts once, in exactly one of these:

| Counter      | The item was                                                                                                                |
| ------------ | --------------------------------------------------------------------------------------------------------------------------- |
| `imported`   | Created.                                                                                                                    |
| `updated`    | Changed by a sync.                                                                                                          |
| `duplicates` | Already imported and left as it was: imported before on a run without `-sync`, or unchanged on a sync.                      |
| `deleted`    | Deleted.                                                                                                                    |
| `kept`       | Left in place: it could not be converted, its existence check returned a permission or fetch error, or its row is orphaned. |
| `moved`      | Moved to another mapped container. The container it arrived in counts it as `imported`.                                     |
| `skipped`    | Deliberately not imported. Does not affect the exit code.                                                                   |
| `failed`     | A write for it failed. The run exits 1.                                                                                     |

- `rejected` is part of `failed`: the items whose source value cannot be
  represented. An item counted in `rejected` is counted in `failed` too.
- `repeated` counts ids the source returned more than once. It appears only
  on `sync plan` lines, and an id is counted once there however often it
  repeats; the repeats are not counted anywhere else.
- A `sync plan` line's `create`, `update`, `unchanged`, `delete`, `moved`
  and `kept` are what the run would count as `imported`, `updated`,
  `duplicates`, `deleted`, `moved` and `kept`; `synced` and `orphaned` count
  rows. The guard's candidate count is a different number: it includes the
  candidates that turn out moved or kept.
- A counter with no meaning for an entity is 0.

## Account creation

- An account listed in `users` is created without a password and added to
  the workspace. Adding it follows [Identity](PROTOCOL.md#identity) rule 9:
  the `workspace_members` row, the personal calendar, its subscription and
  the `workspace.member.added` event in one transaction.
- When sign-in is restricted, by Identity rule 3 — a non-empty configured
  domain list, or at least one enabled `oauth_signin_allowlist` row — the
  address is upserted into `oauth_signin_allowlist` as an `email` row with
  `notes = "created by import<source>"`. An open deployment gets no row:
  writing one would turn the restriction on.
- The person takes the account over on their first OIDC sign-in, by
  Identity rule 2.
- Whether sign-in is restricted is decided read-only during resolution, and
  a dry-run reports it as `sign-in restriction: open`, or as
  `sign-in restriction: domains=<n> allowlist=<n>`, where the counts are the
  configured domains and the enabled allowlist rows.

## Side effects

An event an import writes must not cause anything that reaches a person:
no notification, email, webhook, automation or agent run. Each product
picks the means.

### Time

Time has no in-process event-hook mechanism, so an event its importer
writes has nothing to trigger inside the process. The rule holds by
construction. It stops holding the day such a mechanism is added, and the
importer must then suppress it explicitly.

### Flow

In production the importer is a separate CLI process that registers no
hooks, so nothing in that process reacts to its events; being out of
process is what keeps the rule in production. Every in-process reaction —
notifications, webhooks, event-driven agents, re-evaluation, related-task
suggestions, MCP and browser streams — hangs off `eventbus.fireNotifyHooks`.
The importer does every write with a context marked by
`eventbus.WithoutFanout`, and `fireNotifyHooks` returns at once for such a
context, on both the post-commit path and the event-log bridge. That is what
holds the rule when an import runs in a process that has registered hooks:
a test, or an import run inside the API process.

Another process tailing `events` still sees the rows, so an open browser may
re-fetch. It shows no notification.

## Mapping rules

Each detail contains the phrase listed, and each rule's kind is the name the
common fixture uses for it. `<C>` is the product's container key.

| Rule                                                      | Path                      | Kind                                                           | Phrase                                                           |
| --------------------------------------------------------- | ------------------------- | -------------------------------------------------------------- | ---------------------------------------------------------------- |
| The document is empty                                     | (empty)                   | `empty`                                                        | `mapping is empty`                                               |
| An unknown key                                            | (empty)                   | `unknown_key`                                                  | The YAML library's own message, which names the key              |
| `importer` missing / invalid                              | `importer`                | `required` / `invalid_email`                                   | `is required` / `is not a valid ASCII email address`             |
| `users[i]` email missing / invalid / duplicate            | `users[i].email`          | `required` / `invalid_email` / `duplicate`                     | as above / `is listed twice`                                     |
| A `source_users` key that is not decimal digits           | `source_users["k"]`       | `invalid_source_user_id`                                       | `is not a source user id`                                        |
| A `source_users` value that is invalid                    | `source_users["k"]`       | `invalid_email`                                                | as above                                                         |
| A run with no container                                   | `<C>`                     | `no_containers`                                                | `must list at least one`                                         |
| `source` missing / duplicate                              | `<C>[i].source`           | `required` / `duplicate`                                       | as above                                                         |
| `name` duplicate / longer than 255 runes                  | `<C>[i].name`             | `duplicate` / `too_long`                                       | `is listed twice` / `is longer than`                             |
| A member's email missing / invalid / duplicate / importer | `<C>[i].members[j].email` | `required` / `invalid_email` / `duplicate` / `importer_listed` | as above / `the importer must not be listed`                     |
| A member's role missing / unknown                         | `<C>[i].members[j].role`  | `required` / `invalid_role`                                    | `is required` / `is not one of`, followed by the product's roles |
| An owner setting that names no member (time only)         | The product's own         | `not_member`                                                   | `is neither a member nor the importer`                           |

An empty string counts as missing. `not_member` is not in the common
fixture, because only time has owner settings.

## Fixtures

Both fixtures carry `version: 1`.

### `importers/sync-guard.json`

```json
{
  "version": 1,
  "cases": [{ "name": "…", "present": 0, "synced": 0, "delete": 0, "moved": 0, "refuse": null }]
}
```

`present`, `synced`, `delete` and `moved` are the values of the
[Deletion guard](#deletion-guard), with `delete` the candidate count.
`refuse` is `null`, `"empty-source"` or `"mass-delete"`. The cases cover
`lost` at 10 and 11, `synced` at twice `lost` and one either side, an empty
source with nothing lost, `moved` taking `lost` under the threshold, a first
sync with nothing synced, and an empty source that would also be a mass
delete, which `empty-source` decides.

### `importers/mapping-common.yaml`

```yaml
version: 1
cases:
  - name: <text>
    dry_run: <bool>
    raw: <text> # optional
    mapping: # optional
      importer: <email> # optional
      users: [...] # optional
      source_users: { ... } # optional
    container: # optional; null for none
      source: <text>
      name: <text> # optional
      members: [...] # optional
    expect:
      - { path: <text>, kind: <kind> }
```

- With `raw`, the runner validates that text, as is, as the whole mapping.
  The `empty` and `unknown_key` cases use it. Every `unknown_key` case uses
  the key `authors`, which neither product knows, so the runner checks that
  the detail contains `authors`.
- Without `raw`, the runner puts `container` under its own container key as
  the only element (an empty list when it is `null` or absent), fills in
  the product's other required keys with valid values (flow's `workspace`),
  and validates the result.
- `containers` in an `expect` path stands for the runner's container key.
- A case passes when the set of errors equals the set in `expect`: the path
  matches exactly, and the detail contains the phrase for the kind from
  [Mapping rules](#mapping-rules). `expect: []` means no error.
- Roles are `editor` only, the one role both products have.
- Every kind in [Mapping rules](#mapping-rules) except `not_member` has at
  least one case.
