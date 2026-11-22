# API overview

Huma v2 generates the OpenAPI description automatically. It is served without authentication at `/openapi.json` and `/openapi.yaml`, with a rendered reference at `/docs`. Routes live in `apps/api/internal/http/router/router.go`.

The groups below are not exhaustive; the router is the full list.

- **Public**: `/health`, `/openapi.json`, `/openapi.yaml`, `/docs`; `/auth/*` except `/auth/logout` (register, login, refresh, password-reset, verify-email/confirm, oauth; `dev-login` only in development); `/share/{token}` and `/share/{token}/events`; `/blobs/*`, which carries no session and is authorized by the signature on the URL.
- **Authenticated**: `/auth/logout`, `/user/*`, `/calendars`, `/calendars/{id}/{events,memos,members,invites,albums,labels,activity,export,import,schedule-polls}`, `/search`, and `/invites/{token}/accept`.
- **CalDAV**: `/dav/` (published as `/api/dav/`), authenticated with an app password rather than a session; see [CalDAV](#caldav).
- **Admin**: `/admin/oauth-providers`, `/admin/allowed-emails`, `/admin/instance-admins`.

Sign-in, registration, password reset and OAuth (`/auth/*`) and the public share links (`/share/*`) are rate-limited per client IP; see `TC_AUTH_RATE_LIMIT` and `TC_SHARE_RATE_LIMIT` in [Configuration](configuration.md). CalDAV requests have a budget of their own, `TC_DAV_RATE_LIMIT`. `/health`, the OpenAPI and docs routes and `/blobs/*` are not rate-limited.

## Conventions

The OpenAPI description is the schema; this page covers the rules that hold across operations. The per-operation table (method, success status, `If-Match` guard, list envelope) is in `apps/api/internal/http/router/conventions.go`, `conventions_restore.go` and `conventions_search.go`, and a test keeps the served API in line with it.

### Status codes

| Operation                                                                        | Status       |
| -------------------------------------------------------------------------------- | ------------ |
| Creates a row (`POST` create, register, presign, add member, add instance admin) | 201          |
| Reads, `PATCH`, `PUT`, restore, confirm, accept invite, finalize, cancel         | 200          |
| `DELETE`                                                                         | 204, no body |

Adding a member or administrator who is already one answers 409.

### Errors

Every error body has the same shape; validation failures add an `errors` array locating each field.

```json
{ "status": 412, "code": "EVENT.STALE", "message": "This event changed since you opened it" }
```

Branch on `code`, not on `message`. A request that fails schema validation is 422 `REQUEST.VALIDATION`, including a `limit` outside its range.

Checks run in a fixed order, so the status says which one failed first:

1. Missing `If-Match` (428 `REQUEST.PRECONDITION_REQUIRED`) or malformed input (400).
2. The resource does not exist, or is not visible to the caller (404).
3. The caller may not do this (403).
4. The version is stale (412).

### Conditional writes

A write to a resource that others can edit names the version it replaces.

- Every resource that can be written conditionally carries `etag` in each list item and in the single-resource response, and the single `GET` also returns it as an `ETag` header.
- Send it back as `If-Match`. A missing header is 428, an `If-Match` that is not a valid entity tag is 400 `REQUEST.BAD_IF_MATCH`, and a stale one is 412 with a resource code: `EVENT.STALE`, `CALENDAR.STALE`, `MEMO.STALE`, `COMMENT.STALE`, `CHECKLIST.STALE`, `ALBUM.STALE`, `SCHEDULE_POLL.STALE`, `SCHEDULE_POLL.RESPONSE_STALE`. Weak validators (`W/`) and lists of tags never match; `*` matches any existing row.
- A successful write answers with the new `etag` in the body and the `ETag` header. Reload on 412 and let the user decide again.
- Edits use `PATCH` with merge-patch semantics: an omitted field is unchanged, `null` clears a nullable field, and an array replaces the whole array. Replacing a single value (RSVP, member role, member color, availability sharing, poll answer) is `PUT`.

Guarded: events (`PATCH`, `DELETE`, undoing an occurrence change), calendars, memos, checklist items, comments, album photos, schedule polls (`PATCH`, cancel, finalize, delete) and your own poll answer (`PUT .../response`, whose version is on `GET .../response`).

Unconditional writes, and why:

| Write                                                                                                                          | Reason                                                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------- |
| Creates                                                                                                                        | A new row has no earlier version.                                                                                    |
| Restore a deleted event                                                                                                        | The deleted event can no longer be read, so there is no version to name; a second restore is refused as not deleted. |
| Delete by identity (members, invites, attachments, sessions, allowed e-mails, administrators, OAuth providers, calendar cover) | Nothing is merged.                                                                                                   |
| `PUT` of a single value (RSVP, member role and color, attendee can-edit, availability sharing, avatar)                         | The write replaces the value outright.                                                                               |
| Actions on the caller's own state (`PATCH /user`, password, sessions)                                                          | Nobody else writes it.                                                                                               |
| Confirm of an upload                                                                                                           | It acts on the row the presign reserved.                                                                             |

### Lists

| Envelope              | Used by                                                                                                                                                                         |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `{items, nextCursor}` | Walkable lists: calendar activity, event history, album photos, comments. Pass `nextCursor` back to continue; it is empty at the end.                                           |
| `{items, hasMore}`    | Lists cut by a range or a count: `GET /calendars/{calendarId}/events`, `GET /share/{token}/events`, `GET /search`, member candidates. `hasMore` says the cut dropped rows.      |
| Bare array            | Lists bounded by a limit enforced when rows are written: members, checklist, invites, attachments (100), calendars, instance admins, allowed e-mails (500 each), sessions (50). |

Adding past a cap is refused with a `*.LIMIT_REACHED` 409. Sessions are the exception: the 51st sign-in revokes the oldest one. An export over its range limit is 422 `EXPORT.RANGE_TOO_LARGE`.

`GET /calendars/{calendarId}/events` takes `start` and `end` (dates, `end` inclusive) or `days` (default 30, at most 366), `tz`, and `limit` (default and maximum 5000, earliest kept).

### All-day events

An all-day event is dates, not instants.

- Write and read `startDate` and `endDate` as `YYYY-MM-DD`, with `allDay: true`. `endDate` is exclusive: a one-day event on 2026-03-01 has `endDate` 2026-03-02. Do not send `startAt`, `endAt`, or a time.
- Every viewer sees the same calendar day, whatever their zone. Stored as UTC midnight, the instants `startAt` and `endAt` are those midnights; read the date from `startDate` and `endDate`.
- `timezone` records the author's zone and does not move the date.
- A recurring all-day series has `until` as `YYYY-MM-DD`, and each occurrence starts at UTC midnight; `recurrenceDate` is a date.
- `GET /search` compares an all-day event with `from` by the caller's date, so an all-day event today is still found.

### Visibility

`visibility` is `default` (follow the calendar's setting, and public when that is also default), `public`, `private` or `confidential`. The owner, the creator and the event's attendees always see everything. For other readers:

|                | Calendar member                                                                                            | Anonymous share or embed reader                                      |
| -------------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `public`       | Everything                                                                                                 | Title, times, location; `detailsHidden` is true                      |
| `private`      | Title, times, kind, show-as, owner and attendees; location, memo and URL withheld; `detailsHidden` is true | Times only, as busy time: `title` is `null`, `detailsHidden` is true |
| `confidential` | Not listed; reads answer 404                                                                               | Not listed                                                           |

A response with `detailsHidden: true` is a reduced view. Writing a field that view withholds is refused with 403 `EVENT.DETAILS_HIDDEN`, so a client cannot overwrite a value it never saw.

### Recurring events

Writes on a recurring event take `scope`:

| `scope`     | Effect                                               | `If-Match` compared with    |
| ----------- | ---------------------------------------------------- | --------------------------- |
| `this`      | Changes or cancels one occurrence                    | The occurrence's own `etag` |
| `following` | Edits or deletes this occurrence and every later one | The series (`seriesEtag`)   |
| `all`       | Edits or deletes the whole series                    | The series (`seriesEtag`)   |

- An occurrence item carries `isRecurrence`, `recurrenceDate`, an `etag` for the row its `id` names (its own override if it has one, otherwise the series) and the series' `seriesEtag`.
- `following` splits the series at the occurrence. The earlier part is cut short, and an edit also creates a new series from that occurrence on, which the response returns; a delete answers 204. A split on the first occurrence acts like `all`. A fixed `count` is divided between the two parts by the occurrences the rule generates. If the edit changes the rule or `allDay`, changes to later occurrences are dropped. `following`, and deleting a whole series, are limited to the event's owner and the people who manage the calendar; an attendee who was granted edit rights cannot. `following` is refused on a task-owned event (409 `EVENT.TASK_OWNED`) and on an event that does not repeat (400 `EVENT.NOT_RECURRING`).
- When a `following` edit moves the occurrence to another day and sends no rule, the new series moves with it: a weekly rule's weekdays and a monthly rule's days of the month shift by the same number of days. A shift that takes a day of the month outside 1 to 31 is 422 `REQUEST.VALIDATION`; send the rule you want with the edit instead. A split at the Feb 28 that stands in for Feb 29 returns a monthly rule with an interval of 12 on the 29th, which keeps the same dates.
- Cancelling one occurrence (`DELETE ...?scope=this`) can be undone with `DELETE /calendars/{calendarId}/events/{eventId}/exceptions/{occurrenceDate}` (`YYYY-MM-DD`, guarded by the series `etag`, 204). An occurrence with nothing to undo is 404 `EVENT.EXCEPTION_NOT_FOUND`. A `following` delete cannot be undone.

### Restoring a deleted event

`POST /calendars/{calendarId}/events/{eventId}/restore` brings back a deleted event or series, with its attachments, within 10 minutes of the deletion. It takes no `If-Match`.

| Status | Code                     | Meaning                                                     |
| ------ | ------------------------ | ----------------------------------------------------------- |
| 410    | `EVENT.RESTORE_EXPIRED`  | The 10 minutes are over.                                    |
| 409    | `EVENT.NOT_DELETED`      | The event is not deleted.                                   |
| 409    | `EVENT.RESTORE_CONFLICT` | An imported event with the same UID now occupies its place. |
| 409    | `EVENT.TASK_OWNED`       | A task owns the event.                                      |

### Search

`GET /search?q=&from=&limit=` searches every calendar the caller belongs to and returns `{items, hasMore}`, one item per event: its first occurrence that ends after `from`, ordered by start. `q` is 1 to 100 characters and matches titles, plus locations and memos the caller may read. `from` is an RFC 3339 instant or a `YYYY-MM-DD` date in the caller's zone, and defaults to now. `limit` defaults to 30, at most 100. A query that would scan too many candidates is 422 `SEARCH.TOO_BROAD`; narrow it.

### Uploads

Attachments, album photos, avatars and calendar covers upload the same way, through blob storage rather than the API.

1. Presign (`POST`, 201): send the content type, byte size and `sha256` of the bytes, as 64 lowercase hex characters; anything else is 422. Album photos also send `thumbnailSha256`. The response has an id and `uploadUrl`.
2. `PUT` the bytes to `uploadUrl` (valid 15 minutes). If `uploadUrl` is empty, these bytes are already stored and this step is skipped.
3. Confirm (200; `PUT` for avatars and covers). The server reads the object back and checks it against the declared digest and size: 422 `UPLOAD.DIGEST_MISMATCH`, 413 `UPLOAD.TOO_LARGE`. A failed upload leaves nothing behind.

Storage that is not configured answers 503 `STORAGE.UNAVAILABLE`.

## App passwords

A calendar app cannot hold a session, so it signs in to CalDAV with an app password: one credential per device, shown once and revoked on its own. Changing the account password, resetting it or revoking every session does not touch them. All three operations need a signed-in user and act on that user's passwords only.

| Operation                                    | Status | Body                                                                                                      |
| -------------------------------------------- | ------ | --------------------------------------------------------------------------------------------------------- |
| `GET /user/app-passwords`                    | 200    | Bare array, newest first, revoked ones left out: `id`, `name`, `prefix`, `lastUsedAt`, `createdAt`.       |
| `POST /user/app-passwords`                   | 201    | Takes `name` (1 to 100 characters). Returns the same fields plus `password`, `serverHost` and `username`. |
| `DELETE /user/app-passwords/{appPasswordId}` | 204    | No body. Not `If-Match`-guarded: it changes only the caller's own state.                                  |

- `password` is in the creation response and nowhere else. The server keeps only a SHA-256 digest, so a lost password is replaced, not recovered. `prefix` is the first characters of it, to tell passwords apart in a list.
- `serverHost` is the host of `TC_API_PUBLIC_URL`, the value a calendar app asks for as the server, and `username` is the account's email address.
- Passwords are `ndt_` followed by 32 characters. Spaces, hyphens and letter case are ignored when one is presented, so the grouped form the web app displays can be typed as shown.

| Status | Code                            | Meaning                                                                                                     |
| ------ | ------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| 403    | `APP_PASSWORD.EMAIL_UNVERIFIED` | The account has a local sign-in whose email is not yet confirmed.                                           |
| 403    | `APP_PASSWORD.MFA_ENABLED`      | The account's local sign-in has a second factor. CalDAV accepts the password alone, so it would be refused. |
| 409    | `APP_PASSWORD.LIMIT_REACHED`    | The user already has 20 unrevoked passwords.                                                                |
| 404    | `APP_PASSWORD.NOT_FOUND`        | Revoking a password that does not exist, belongs to someone else or is already revoked.                     |

## CalDAV

The CalDAV server lets a calendar app (iOS and macOS Calendar, or any client that speaks CalDAV) read and edit the calendars its user belongs to. It is mounted at `/dav/` on the API, so `/api/dav/` behind the bundled nginx; the address to give a client is `TC_API_PUBLIC_URL` followed by `/dav/`.

### Authentication and discovery

- Authentication is HTTP Basic over TLS. The password is an app password; the username is not checked, so a change of email address does not break a device. Anything else, including an unknown, revoked or malformed credential, is 401 with `WWW-Authenticate: Basic realm="<TC_APP_NAME>", charset="UTF-8"`. A password also stops working when its user is disabled, when the user has no enabled sign-in, or when the local sign-in has a second factor enrolled. A sign-in lockout from failed password attempts does not affect it.
- `OPTIONS` is answered without credentials: 204 with `DAV: 1, 3, calendar-access`.
- `/.well-known/caldav` redirects (301) to `/api/dav/` in the bundled nginx, so a client given only a host name finds the server. Clients that skip discovery take the full address.
- The paths are `/dav/<userId>/` (the principal), `/dav/<userId>/calendars/` (the calendar home) and `/dav/<userId>/calendars/<calendarId>/` (one calendar). Another user's principal and a calendar the user does not belong to are 404.

### What maps to what

- Every calendar the user is an active member of is a calendar collection under the home, with the same membership the REST API uses. It holds `VEVENT` only.
- A calendar is read-only (no `DAV:write` privilege) when the user's role cannot edit it and for system calendars.
- One event, with all of its occurrence changes, is one resource named `<name>.ics`. The resource holds the series and every changed occurrence the reader may see, and shows each exactly as that reader's iCalendar export does, including the hiding rules in [Visibility](#visibility); a confidential event is absent for anyone who is not a party to it. Events the client created keep the name it chose; others are named `ndt-<id>.ics`. A name is 1 to 251 characters from `A-Z a-z 0-9 . _ ~ @ + = -` plus `.ics`, compared case-sensitively; anything else is 400.
- The `ETag` is a hash of what the reader would see, so it changes when the visible content does and not otherwise.
- Labels, assigned layers, attendees, checklists, attachments, comments and the creator are not carried. A write from a calendar app leaves them as they are.
- Calendar-query and multiget are supported. A calendar with more than 20,000 entries answers 507.

### Sync

The server advertises `sync-collection` and a `DAV:sync-token` on each calendar, built from the calendar's change history. An empty token returns every resource; a later one returns what changed and what was deleted.

- A token is accepted for the UTC day it was issued on. From the next day, or after the calendar's settings or the user's own role changed, or after more than 5,000 changes, or when it is malformed or from another calendar, the answer is 403 `valid-sync-token`, and the client starts over with an empty token. A client therefore does a full pass at least daily; this is also what removes events that left the reader's view without being deleted.
- A `limit` the result exceeds is 507 `number-of-matches-within-limits`.
- Changes to confidential events a reader may not see are not reported to that reader.

### Writes

A `PUT` is turned into the REST operations that make the same change, so permissions, validation and the change history behave as they do for the web app.

- `If-None-Match: *` creates an event. `If-Match` replaces the one the client read: a stale tag is 412. `DELETE` removes the whole series. A `PUT` answer carries no `ETag`, because what is stored can differ from what was sent; the client reads it again.
- Only changed properties are applied: summary, location, description, URL, start and end, all-day, time zone, recurrence rule, class, transparency and the first alarm that can be expressed. A new `RECURRENCE-ID` changes that occurrence, an added `EXDATE` cancels one and a removed one restores it.
- Anything the REST API would refuse is 403 with no precondition element: editing someone else's event as an editor, changing the time of a task-linked event, writing to a read-only calendar. A body the server cannot accept is 409 with a CalDAV precondition: `valid-calendar-data`, `supported-calendar-component` (anything but `VEVENT`), `no-uid-conflict` (a `UID` that differs from the resource's, or that another visible event in the calendar already uses), `max-resource-size` (over 1 MiB). An empty or non-UTF-8 body is 400.
- Refused as `valid-calendar-data`: `RDATE`, `STATUS:CANCELLED`, `RANGE=THISANDFUTURE`, `METHOD`, more than one `UID`, more than 1,000 `EXDATE`s, a negative duration, a recurrence rule the server does not store (for example a plain monthly rule starting on the 29th to 31st, or one with `BYMONTH`), and values the REST API's own limits reject.
- Floating times and unknown time zones are read as UTC. Names that start with `ndt-` and UIDs ending in `@nodate-time` are reserved for events the server named; creating one is 403.
- Properties the server does not use (`SEQUENCE`, `GEO`, `ATTACH`, `PRIORITY`, `COLOR`, `X-` properties and so on) are ignored. `ATTENDEE` and `ORGANIZER` are neither written nor read.

### Not supported

- `MKCALENDAR`, `MKCOL`, `COPY` and `MOVE` are 403, and `PROPPATCH` is a 207 refusing each property it names with 403: calendars are created, renamed, recolored and deleted in the web app, and an event cannot be moved between calendars from a client.
- Scheduling (invitations, free-busy), to-dos, notes, address books, push notifications and `CS:getctag`.
- A read-only scope per password: every password carries its user's full access.

### Limits

A resource is at most 1 MiB. `TC_DAV_RATE_LIMIT` caps requests per client address per minute, `600` by default. A user holds at most 20 app passwords. Each REPORT's kind is logged as `dav.report`.
