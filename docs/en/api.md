# API overview

Huma v2 generates the OpenAPI description automatically. It is served without authentication at `/openapi.json` and `/openapi.yaml`, with a rendered reference at `/docs`. Routes live in `apps/api/internal/http/router/router.go`.

The groups below are not exhaustive; the router is the full list.

- **Public**: `/health`, `/openapi.json`, `/openapi.yaml`, `/docs`; `/auth/*` except `/auth/logout` (register, login, refresh, password-reset, verify-email/confirm, oauth; `dev-login` only in development); `/share/{token}` and `/share/{token}/events`; `/blobs/*`, which carries no session and is authorized by the signature on the URL.
- **Authenticated**: `/auth/logout`, `/user/*`, `/calendars`, `/calendars/{id}/{events,memos,members,invites,albums,labels,activity,export,import,schedule-polls}`, `/search`, and `/invites/{token}/accept`.
- **Admin**: `/admin/oauth-providers`, `/admin/allowed-emails`, `/admin/instance-admins`.

Sign-in, registration, password reset and OAuth (`/auth/*`) and the public share links (`/share/*`) are rate-limited per client IP; see `TC_AUTH_RATE_LIMIT` and `TC_SHARE_RATE_LIMIT` in [Configuration](configuration.md). `/health`, the OpenAPI and docs routes and `/blobs/*` are not rate-limited.

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
