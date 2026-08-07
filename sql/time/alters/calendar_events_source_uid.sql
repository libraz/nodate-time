-- ====================================
-- calendar_events.source_uid — recognise a calendar file already imported
--
-- Importing the same file twice used to duplicate everything in it, and
-- uploading again is precisely what somebody does when they are not sure the
-- first attempt worked. Nothing on the row remembered what the file had
-- called the event, so every import was a first import.
--
-- Only a series head or a plain event carries a UID here. A changed
-- occurrence shares its series' UID by design -- that is what RECURRENCE-ID
-- means -- so putting overrides in the same namespace would make the
-- constraint reject exactly the files that use the feature correctly. They
-- need no UID of their own: uniq_calendar_events_recurrence_override already
-- identifies an override by its parent and the occurrence it replaces, which
-- is a stronger identity than the UID could give it. Overrides leave this
-- column NULL.
--
-- The uniqueness is scoped by calendar, not by workspace. A calendar_id
-- already implies a workspace, and adding workspace_id to a UNIQUE widens the
-- key, which weakens it. The workspace-scoped variant of the public_id key
-- exists for tenant-scoped lookups; it is not a model to copy here.
--
-- source_uid_key is why the constraint can be scoped to live rows. This table
-- soft-deletes solely through enabled = FALSE, so a plain UNIQUE would let a
-- deleted event keep its UID reserved on that calendar forever: delete an
-- event, re-import the file that has it, and it could never come back.
-- NULLing the key when the row is disabled leans on the same MySQL rule
-- task_role_key already uses in this table -- NULLs in a UNIQUE are distinct
-- -- just pointed the other way: task_role_key de-NULLs a value to bring it
-- INTO a constraint, this one NULLs a value to take it out.
--
-- The collation is deliberately not the utf8mb4_0900_ai_ci the rest of the
-- schema uses. A UID is opaque, and under an accent- and case-insensitive
-- collation two genuinely different UIDs would compare equal and one event
-- would be mistaken for another. utf8mb4 rather than latin1 because RFC 5545
-- gives the UID value no ASCII restriction.
--
-- A UID longer than 255 characters is stored as NULL rather than truncated,
-- so it simply has no import identity and duplicates on re-import as before.
-- Truncating would let two different UIDs share a prefix and merge two
-- unrelated events. When in doubt take the recoverable failure: failing
-- toward a duplicate is recoverable and failing toward a merge is not.
--
-- KNOWN LIMITATION. A UID is only unique within the file that supplied it, so
-- importing two unrelated source calendars into one calendar here can collide
-- on a naive UID such as `1@example.com` and treat the second file's event as
-- already present -- and that event then never appears at all, which is worse
-- than showing stale data. The import counter is what makes it survivable:
-- the event is reported, not dropped in silence. Scoping by a source
-- discriminator taken from PRODID or X-WR-CALNAME would be worse, because
-- those change when the same calendar is exported from a different client,
-- which is precisely when this column has to keep working.
--
-- It lives here rather than in core/ because the contract has already said
-- what an event is. Recognising a file this calendar has seen before is this
-- product's import policy.
-- ====================================
ALTER TABLE calendar_events
  ADD COLUMN source_uid VARCHAR(255)
    CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs
    NULL
    COMMENT 'UID of the VEVENT this event was imported from, as the file gave it. NULL when the event was created here, when it is a recurrence override, or when the file UID exceeded the column.',
  ADD COLUMN source_uid_key VARCHAR(255)
    CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs
    GENERATED ALWAYS AS (IF(enabled, source_uid, NULL)) STORED
    COMMENT 'source_uid while the row is live, NULL once it is soft-deleted. Exists solely to scope uniq_calendar_events_source_uid to rows the calendar still shows. Generated: never written directly.',
  ADD UNIQUE KEY uniq_calendar_events_source_uid (calendar_id, source_uid_key);
