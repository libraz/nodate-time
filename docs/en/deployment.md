# Deployment

`compose.prod.yml` runs the stack (MySQL, object storage, the API and the built SPA) on a single host. It builds the two images rather than pulling them: the SPA compiles the API origin into its bundle, so no prebuilt image is correct for two deployments.

![Architecture](../images/architecture.svg)

One hostname, one certificate, and nothing else published. The web service is the only way in: it terminates TLS, serves the SPA, and passes `/api` to the API over the compose network. Neither the API nor object storage needs a route from the internet, and neither is given one. Attachments, album photos and avatars are relayed by the API, which hands the browser a short-lived signed URL back to itself and copies the bytes through.

## TLS certificate

The certificate has to exist before the stack starts; nginx will not load a server block whose certificate is missing. Obtain one first, with port 80 free:

```sh
mkdir -p acme
certbot certonly --standalone -d app.example.com
```

Renewal is answered by the running server out of `acme/`, so nothing has to be stopped for it. nginx keeps the old certificate until it is told to look again:

```sh
certbot renew --webroot -w /path/to/acme \
  --deploy-hook 'docker compose -f /path/to/compose.prod.yml exec web nginx -s reload'
```

`TC_API_PUBLIC_URL` has to carry the `/api` prefix (`https://app.example.com/api`): it is the address the OAuth callback returns to, the address every blob URL points back at, and the path the `$schema` links in responses are built from. `TC_TRUSTED_PROXIES` defaults to `172.16.0.0/12`, the range Docker gives a compose network. If this one was given an address outside it, set the range yourself, or every client collapses onto one rate-limit bucket, since all of them arrive through nginx.

To terminate TLS somewhere else instead, such as a reverse proxy on the host or a tunnel, drop the TLS template mount and the two certificate volumes from the `web` service and publish `127.0.0.1:8081:80`. The image then serves plain HTTP and still proxies `/api` to the API itself, so the arrangement above is unchanged from the browser's side; point the host's proxy at that one port.

## Configuration

Configuration goes in a `.env` beside `compose.prod.yml`. `TC_DB_ROOT_PASSWORD`, `TC_DB_PASSWORD`, `TC_JWT_SECRET` (`openssl rand -hex 32`), `TC_WEB_URL`, `TC_API_PUBLIC_URL`, `TC_PUBLIC_HOST`, `TC_S3_ACCESS_KEY` and `TC_S3_SECRET_KEY` have no safe default: Compose stops and names whichever is missing. `TC_SMTP_HOST` is optional: without a relay no mail is sent, and password reset and email verification are not offered. Every other variable in the [Configuration](configuration.md) table is passed through too, with the same default, except:

- `TC_CORS_ALLOWED_ORIGINS` is always `TC_WEB_URL`; a value in `.env` is ignored.
- `TC_PORT` is not passed: the API listens on 8080 inside the container, and `TC_API_PORT` (default 8080) chooses the host port on loopback.
- `TC_METRICS_ADDR` defaults to `:9464`, because a container cannot bind the host's loopback; the published port is what stays on `127.0.0.1` (`TC_METRICS_PORT`, default 9464).
- `TC_ENV` is always `production`, and `TC_DB_DSN` is assembled from `TC_DB_USER`, `TC_DB_PASSWORD` and `TC_DB_NAME`.

`TC_SECRETS_KEY` is optional. Without it the admin panel refuses to save OAuth client secrets rather than storing them unencrypted.

Every page the web service serves carries a Content-Security-Policy that forbids framing, `nosniff` and `Referrer-Policy: no-referrer`, plus HSTS when it terminates TLS itself. `/embed/*` is the one exception and may be framed by any site. The policy allows scripts and connections from the service's own origin only, so an API served from another origin needs `connect-src` widened in `apps/web/nginx/security-headers.conf`.

## Starting the stack

```sh
docker compose -f compose.prod.yml up -d --build
docker compose -f compose.prod.yml run --rm --entrypoint /createuser api \
  -email you@example.com -password 'choose-a-long-one' -name "Your Name" -admin
```

With `TC_PASSWORD_LOGIN_ENABLED=false`, leave out `-password`. The account is then created without one, and the first Google sign-in whose verified email matches claims it, administrator rights included. An unconfirmed account that holds a password is never linked by a provider, because whoever registered the address could have set it.

The schema is applied when the MySQL volume is first created. `sql/schema.sql` drops every table before creating it, so it is never re-run against a database that holds data; see [Upgrading](#upgrading) for how a schema change reaches one.

## Calendar apps (CalDAV)

The API serves CalDAV at `/dav/`, published as `/api/dav/` by the bundled nginx. Users sign in to it with app passwords created in Settings; the protocol is described in [API overview](api.md#caldav). Nothing has to be enabled: the endpoint exists on every deployment, and `TC_DAV_RATE_LIMIT` (see [Configuration](configuration.md)) only sets its per-client budget.

- The bundled nginx answers `/.well-known/caldav` with a 301 to `/api/dav/`, without authentication, so a calendar app given only the host name finds the server. A proxy or tunnel you put in front of the web service has to pass that path through unchanged.
- Apple's clients also treat the host root as the CalDAV server and ignore a redirect from it, so the bundled nginx serves `OPTIONS`, `PROPFIND` and `REPORT` on `/` as `/api/dav/` in place. Other methods on `/` reach the web app as before. A proxy in front of the web service has to pass those methods on `/` through.
- Whatever sits in front of the API, whether the bundled nginx, a host proxy, a tunnel or a CDN, has to pass the WebDAV methods `PROPFIND`, `REPORT`, `PUT`, `DELETE` and `OPTIONS` and the `Authorization` header. A layer that rejects unknown methods or strips the header shows up as a calendar app that cannot sign in or sync; a `PROPFIND` answered with 403 or 405 is the usual sign. The bundled nginx does not restrict methods under `/api/`.
- Credentials travel in HTTP Basic, so the endpoint has to be reached over TLS.
- Check the redirect and the method pass-through from outside the stack: `curl -i https://app.example.com/.well-known/caldav` answers 301 with `Location: /api/dav/`, and `curl -i -X PROPFIND https://app.example.com/api/dav/` answers 401 with a `WWW-Authenticate: Basic` header.
- The `caldav.sql` step of [Upgrading](#upgrading) creates the tables behind app passwords and the client-chosen resource names; a database that lacks them fails on CalDAV requests and on the app password endpoints.

## Backups

Two volumes hold everything durable: `mysql_data` and `storage_data`. Back them up together, because an attachment is a row in one pointing at an object in the other. Objects are plain files whose content type and ETag live in extended attributes, so a file-level copy has to keep them (GNU `tar --xattrs --xattrs-include='user.*'`; busybox tar drops them).

A logical dump of the database alone:

```sh
docker compose -f compose.prod.yml exec -T mysql \
  sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction "$MYSQL_DATABASE"' \
  > backup.sql
```

## Upgrading

Back up both volumes first, as described in [Backups](#backups). A database that holds data is never rebuilt: `sql/schema.sql` and `sql/core/tools/build-schema.sh` drop every table they create, so applying either to a live database wipes it. It is brought forward by upgrade files instead, in this order, with the API and every other product writing the same database stopped:

1. Classify the all-day rows. Run `sql/core/tools/classify-all-day.sql` against the database; it only reads. Set `@nf_http_cutoff` first, to the UTC time at which the other product's normalisation of all-day rows reached this database. The report lists how each all-day row will be converted and ends with one `INSERT` per row that needs a decision. Fill in the form and zone of those rows and run the statements: they go into `core_upgrade_0004_decisions`, a table that step 2 creates on its first run.
2. `sql/core/upgrades/*.sql`, every file, in number order. These change the tables shared with other products and always come first; the later steps refuse to run until they have. `0004-all-day-canonical.sql` converts every all-day row to its canonical form and adds the constraints that enforce it. It stops, listing the rows by `public_id`, while any row is undecided or any other finding remains; settle those and run it again. Apply it without `--force`.
3. `sql/time/upgrades/from-v0.1.0.sql`, only on a database that has not had it, which is one without the `album_photos.retired_at` column. Once the new API has served a request it is never run again.
4. The feature files, in this order: `schedule-polls.sql`, `calendar-covers.sql`, `caldav.sql`. Each adds only what is missing and replaces views, so it applies to any database past step 3.
5. `sql/time/upgrades/triggers.sql`, after the feature files. It drops and recreates every trigger of this layer. MySQL cannot replace a trigger atomically, so a delete running in between would miss it; this is why every writer is stopped.
6. The conformance check, `sql/core/conformance/run.sh --mode all`, which confirms the result matches the contract. Its data checks need step 2 applied.
7. Start the new API.

```sh
docker compose -f compose.prod.yml stop api
docker compose -f compose.prod.yml cp sql mysql:/tmp/upgrade-sql

# Step 1: read-only report. Repeat until no row is left undecided.
docker compose -f compose.prod.yml exec -T mysql bash -euc '
  export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
  mysql -uroot --table "$MYSQL_DATABASE" -e "SET @nf_http_cutoff = '"'"'2026-01-01 00:00:00'"'"'; source /tmp/upgrade-sql/core/tools/classify-all-day.sql"
'

# Steps 2 to 7. Stops at the first file that fails; if 0004 stops on undecided rows, record the decisions and run this block again.
docker compose -f compose.prod.yml exec -T mysql bash -euc '
  export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
  up=/tmp/upgrade-sql/time/upgrades
  apply() { echo "applying ${1#/tmp/upgrade-sql/}"; mysql -uroot "$MYSQL_DATABASE" < "$1"; }
  for f in /tmp/upgrade-sql/core/upgrades/*.sql; do apply "$f"; done
  if ! mysql -uroot "$MYSQL_DATABASE" -e "SELECT retired_at FROM album_photos LIMIT 0" 2>/dev/null; then
    apply "$up/from-v0.1.0.sql"
  fi
  for f in schedule-polls calendar-covers caldav triggers; do apply "$up/$f.sql"; done
  bash /tmp/upgrade-sql/core/conformance/run.sh \
    --dsn "root:$MYSQL_ROOT_PASSWORD@127.0.0.1:3306/$MYSQL_DATABASE" --mode all
  rm -rf /tmp/upgrade-sql
' && docker compose -f compose.prod.yml up -d --build
```

The password and database name are read inside the container, from the values `.env` gave it, so nothing has to be exported on the host. The `@nf_http_cutoff` value in the report command is a placeholder; use your own. The script stops at the first file that fails, and the rebuild runs only if every file and the check succeeded. Stop the old API first: a row it deletes after the upgrade has run would read as an upload still awaiting confirmation. Each step checks what is already there, so a run that stopped partway can be repeated, as long as the new API has not yet served a request.

### Allowed sign-in addresses turn the restriction on

The upgrade moves the admin panel's allowed e-mail addresses into the sign-in allowlist the database shares with other products. Any enabled entry there restricts OAuth sign-in to the listed addresses and domains, whether or not `TC_GOOGLE_ALLOWED_DOMAINS` is set. A deployment that left the domain list empty and listed a few addresses as exceptions let everyone sign in before the upgrade; after it, only those addresses can. Withdraw the entries in the admin panel if that is not what you want.

### Rows rewritten once

The upgrade also rewrites some existing rows once:

- activity-log entries take the event names and payload keys shared with other products;
- e-mail addresses are stored lower-case;
- every session is marked as issued by this application;
- storage reference counts are recounted;
- weekly repeats keep the Sunday week start they were shown with.

### Coming from v0.1.0

The deployment itself also changed:

- The web service terminates TLS and publishes 80 and 443 instead of `127.0.0.1:8081`. Obtain a certificate before starting, as described in [TLS certificate](#tls-certificate), or keep the proxy on the host by following the paragraph on terminating TLS elsewhere.
- `TC_PUBLIC_HOST`, the hostname the certificate is issued for, is required.
- `TC_SMTP_HOST` and `TC_SMTP_FROM` are optional. Without a relay, mail is off and password reset and email verification are not offered.
- `TC_TRUSTED_PROXIES` defaults to `172.16.0.0/12`, the range Docker gives a compose network.
- Object storage moved from MinIO (`minio` service, `minio_data` volume) to the Versity S3 Gateway (`storage` service, `storage_data` volume). The formats differ, so the objects are copied over S3 rather than the volume being reused; see [Moving objects off MinIO](#moving-objects-off-minio).

## Object storage

The bundled store is the Versity S3 Gateway over a plain directory: single node, no erasure coding, no versioning, no replication. Durability is the underlying volume plus the backup above, which is the same footing the database is on. It answers only on the private network, and `TC_S3_*` can point at any other S3-compatible endpoint instead without anything above the configuration changing: the API reaches the store, the browser never does, either way.

### Moving objects off MinIO

A deployment from before the switch keeps its objects in `minio_data`, in MinIO's own on-disk format, so they are copied over S3 by the `mc` client inside the old container, which keeps running after the new compose file no longer defines it. Stop the API and apply the upgrades as above, but hold the rebuild until the objects have moved:

```sh
B=nodate-time   # TC_S3_BUCKET
docker compose -f compose.prod.yml up -d --wait storage
OLD=$(docker ps -q --filter label=com.docker.compose.service=minio)
docker exec -e B="$B" "$OLD" sh -c '
  mc alias set old http://localhost:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc alias set new http://storage:7070 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" &&
  mc mb --ignore-existing "new/$B" &&
  mc mirror --preserve "old/$B" "new/$B"'
echo "SELECT storage_key, content_type FROM storage_objects WHERE storage_key LIKE 'albums/%.thumb'" |
  docker compose -f compose.prod.yml exec -T mysql \
    sh -c 'exec mysql -N -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"' |
  while read -r key type; do
    docker exec -e B="$B" -e P="${key%.thumb}" -e T="$type" "$OLD" sh -c '
      mc cat "old/$B/$P/thumb" > /tmp/thumb &&
      curl -sf --aws-sigv4 aws:amz:us-east-1:s3 --user "$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD" \
        -H "Content-Type: $T" -T /tmp/thumb "http://storage:7070/$B/$P.thumb"' </dev/null ||
      echo "not copied: $key"
  done
docker compose -f compose.prod.yml up -d --build --remove-orphans
```

Thumbnails need the second step. v0.1.0 stored one underneath its photo (`<photo>/thumb`), a key the new store cannot hold beside the photo itself, and MinIO does not list a key that sits under another object, so `mc mirror` never sees them. The upgrade file has already moved their rows to `<photo>.thumb`; the loop reads that list and copies each one to its new key. A `not copied` line names a thumbnail the old store did not have.

Both stores take the same `TC_S3_*` credentials, so the old container's own variables serve for both. Keep the `minio_data` volume until attachments, album photos and avatars have been checked in the running app, then remove it with `docker volume rm`.

## Migrating from TimeTree

`importtimetree` copies shared calendars out of TimeTree and keeps track of whose event each one is. It logs in to TimeTree itself, fetches every calendar and event up front, then creates the accounts, calendars and members a mapping file describes and imports the events under the owners it assigns. It ships in the API image as `/importtimetree`.

Before you start:

- Every email in the mapping must be the address that person signs in with through Google. The accounts are created without passwords, and the first Google sign-in whose verified email matches claims one. An address nobody can sign in with leaves an account nobody can reach.
- Set `TIMETREE_EMAIL` and `TIMETREE_PASSWORD` for the command (see [Configuration](configuration.md)). They are read from the environment so they never show up in the process list.

### Mapping file

```yaml
importer: ops@example.com # runs the import; owns the calendars and is the default owner
users: # accounts to create (existing ones are skipped)
  - email: hanako@example.com
    name: Hanako Yamada # defaults to the local part of the email
source_users: # TimeTree user id -> email of who created the events
  10000001: hanako@example.com
calendars:
  - source: Band # the TimeTree calendar's name or alias code
    name: Band # name here; defaults to the TimeTree name
    default_owner: mgr@example.com # owner of events with no mapped label; defaults to importer
    default_attendees: [hanako@example.com] # participants of those same events; optional
    members:
      - { email: hanako@example.com, role: viewer }
      - { email: mgr@example.com, role: manager }
    labels: # TimeTree label name -> owner's email
      Hanako: hanako@example.com
```

- `importer` is the existing or newly created account the import runs as. It becomes the owner of every calendar, so it must not be listed under `members`.
- `users` lists the accounts to create. An address that already has an account is left alone.
- `calendars[].source` selects one active TimeTree calendar by name or alias code.
- `calendars[].name` is the calendar's name here. An existing calendar of that name that the importer belongs to (other than a personal one) is reused.
- `members` gives each person a role: `manager`, `editor` or `viewer`. An existing member keeps the role they have.
- `labels` maps TimeTree label names to the person who owns those events. Events with no label, or a label not listed, go to `default_owner`.
- `default_attendees` makes the listed people participants of the events that go to `default_owner`, such as a whole group's rehearsals. Each must be a member or the importer. Events under a mapped label get no participants. Participants count as busy in availability searches, and an event with two or more participants shows a plain dot instead of its owner's picture.
- `source_users` maps a TimeTree user id to the account shown as the creator of the events that user created. The id is the same in every TimeTree calendar, and the dry run lists each calendar's authors with their ids. An event is filed under the importer as its creator when its author is not listed, or is not a member of that calendar here, as happens with people who have left. The creator does not decide the owner: that is still the label's.

Everything is checked before anything is written, and any of these stops the command with nothing written:

- an unknown key, a duplicate entry or an unknown role
- a malformed or non-ASCII email
- an owner who is not a member of the calendar
- a `source` value that matches no calendar, or more than one
- a mapped label that no TimeTree label matches
- a `source_users` id that is not a user of any mapped TimeTree calendar
- two existing calendars with the same name

### Dry run first

```sh
docker compose -f compose.prod.yml run --rm -T -e TIMETREE_EMAIL -e TIMETREE_PASSWORD \
  --entrypoint /importtimetree api -mapping - -dry-run < mapping.yaml
```

`-mapping` takes a path or `-` for standard input. With `-dry-run` the command logs in, fetches and validates, prints the report and writes nothing. For each TimeTree calendar the report shows the event count, the earliest start date, the count per label (including events with no label), the count per TimeTree author by id and name, what would be skipped and why, the number of events that have attendees, the number of timed events recorded in UTC, and three all-day events with their raw and converted times. For each calendar in the mapping it adds the count per owner and per creator, the authors that would be recorded as the importer and why, and whether the calendar would be created or reused. After the calendars come the sign-in restriction (`open`, or the number of allowed domains and allow-listed addresses), the accounts that would be created, and the validation errors, each given with the path of the mapping entry it concerns. Read the label and owner counts for a label missing from the mapping, and compare the earliest dates and the all-day examples with what TimeTree shows.

### Running it

```sh
docker compose -f compose.prod.yml run --rm -T -e TIMETREE_EMAIL -e TIMETREE_PASSWORD \
  --entrypoint /importtimetree api -mapping - < mapping.yaml
```

Export `TIMETREE_EMAIL` and `TIMETREE_PASSWORD` in the shell first; `-e` passes them into the container without putting them on the command line. The mapping file reaches the container on standard input.

| Exit code | Meaning                                                                                                                                                    |
| --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0         | Success.                                                                                                                                                   |
| 1         | One or more events failed while writing. Others were imported.                                                                                             |
| 2         | Nothing was written: bad input, missing credentials, a validation error, a failed login or fetch, or a sync the deletion guard refused. Safe to run again. |

Each calendar gets a line, followed by the totals:

```text
calendar "Band" (reused): members added=0 existing=9; events imported=1 updated=3 duplicates=120 deleted=2 kept=0 moved=0 skipped=0 failed=0 rejected=0
total: events imported=1 updated=3 duplicates=120 deleted=2 kept=0 moved=0 skipped=0 failed=0 rejected=0; calendars_created=0 users_created=0
```

Events are identified by their TimeTree id, so running the same import again without `-sync` imports nothing new and reports the already-imported events as duplicates. `updated`, `deleted`, `kept` and `moved` stay 0 without `-sync`.

### Following TimeTree with `-sync`

While people still edit in TimeTree, `-sync` makes the events this command imported follow it. TimeTree wins:

- An imported event that changed in TimeTree is rewritten here: title, times, repeat rule, cancelled dates, location, memo, URL, owner and participants. Edits made here to those fields are overwritten, and occurrences edited one at a time here are dropped.
- An imported event that TimeTree deleted, or no longer returns, is deleted here the same way deleting it in the app does.
- Visibility, busy/free and reminders have no TimeTree counterpart and keep what they were set to here.
- Events created here and events imported from an iCal file are never touched.
- An imported event deleted here comes back if TimeTree still has it.
- Members are only added. Someone removed here who is still in the mapping is added again; take them out of the mapping to keep them out.

Always read the plan first, and on the first sync check `update=` in particular: a changed mapping or workspace time zone shows up as a large number of updates.

```sh
docker compose -f compose.prod.yml run --rm -T -e TIMETREE_EMAIL -e TIMETREE_PASSWORD \
  --entrypoint /importtimetree api -mapping - -sync -dry-run < mapping.yaml
```

With `-sync`, the dry run adds one line per mapped calendar:

```text
calendar "Band": sync plan events create=1 update=3 unchanged=120 delete=2 moved=0 kept=0 synced=125 orphaned=0 repeated=0
```

- `delete` counts the imported events TimeTree no longer has; `moved` is the part of them that is now in another mapped calendar, where it is created again.
- `kept` counts events TimeTree still has that will be left as they are: ones that no longer convert, and ones with the same id that this command did not import.
- `synced` is the imported events found; `orphaned` is events carrying an id with no record of having been imported by this command. Those are never updated or deleted.
- `repeated` counts events TimeTree returned more than once; the last copy is used.

Before writing anything, the sync checks each calendar. If it would delete more than 10 events and more than half of the calendar's synced events (moves not counted), or TimeTree returned no events for a calendar that has synced ones, it prints `refusing: ...` and exits with 2, dry run included. A partial fetch or a login to the wrong TimeTree account looks exactly like that. Find the cause and run again.

Then run it without `-dry-run`. In the report, `duplicates` counts the events that were already up to date.

### What is not migrated

Attendees, comments, attachments, reminders, memos, birthdays, public calendars and deactivated calendars are not imported; the report counts what it skips. Calendar colors take the default. Events keep only the owner the label gives them.

### Re-running without `-sync`

Without `-sync` the importer does not update what it has already imported. Run it again after people have started using the app and it brings back events they deleted and members they removed. Either way, a calendar renamed here is created again under the mapped name, and the old one is no longer synced.
