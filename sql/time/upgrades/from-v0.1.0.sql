-- Upgrades the time layer of a database created by v0.1.0, or by a later
-- build of main, to the current schema, keeping its data.
--
-- Apply it after sql/core/upgrades/*.sql, which reshape the shared tables
-- this file then rewrites rows in; it refuses to run before them. Stop the
-- API first and start the new one only after `run.sh --mode schema` passes.
--
-- Rows rewritten once: event-log kinds and payload keys renamed to the core
-- registry's, subject_public_id cleared on kinds that have no subject, weekly
-- rules given the Sunday week start they were displayed with, allowed e-mail
-- addresses moved into oauth_signin_allowlist, e-mail addresses lower-cased,
-- sessions stamped with this application's audience, storage ref_count
-- recounted from the rows that point at each object, deleted attachments and
-- photos marked retired, comment log rows stripped of the comment text, and
-- thumbnail storage keys moved from <photo>/thumb to <photo>.thumb (the
-- objects themselves are renamed when copied; see the README).
--
-- Every step checks what is already there, so an interrupted run can be run
-- again, and so can one applied too early: run it once more after the old
-- API has stopped and before the new one starts. Once the new API has served
-- a request, do not run it again -- confirming an upload also touches
-- updated_at, which is what the retired marking reads.

SET NAMES utf8mb4;

-- updated_at is a TIMESTAMP, shown in the session's zone, and created_at is a
-- DATETIME the API wrote in UTC. The retired marking compares them.
SET time_zone = '+00:00';

DROP PROCEDURE IF EXISTS upgrade_from_v0_1_0;

DELIMITER //

CREATE PROCEDURE upgrade_from_v0_1_0()
BEGIN
  -- The payload rewrites drop $.id, which core 0001 reads to fill the plain
  -- subject column; run before it, they would lose every subject.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND COLUMN_NAME = 'subject_public_id'
      AND COLUMN_TYPE = 'binary(16)' AND GENERATION_EXPRESSION = ''
  ) OR NOT EXISTS (
    SELECT 1 FROM information_schema.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'oauth_signin_allowlist'
  ) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'from-v0.1.0: apply sql/core/upgrades/*.sql in number order first';
  END IF;

  -- ---------------------------------------------------------------
  -- album_photos, and the rows v0.1.0 deleted
  -- ---------------------------------------------------------------
  -- A deleted photo is told apart from an upload that was never confirmed:
  -- both have enabled = FALSE, and retired_at is set only on the deleted one.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'album_photos'
      AND COLUMN_NAME = 'retired_at'
  ) THEN
    ALTER TABLE album_photos
      ADD COLUMN retired_at DATETIME(3) NULL
        COMMENT 'When a confirmed photo was deleted. NULL on a live photo and on an upload that was never confirmed, which is what keeps a deleted photo from confirming back.'
        AFTER enabled;
  END IF;

  -- The index carried a comment naming a query it does not serve.
  IF EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'album_photos'
      AND INDEX_NAME = 'idx_album_photos_enabled_created' AND INDEX_COMMENT <> ''
  ) THEN
    ALTER TABLE album_photos
      DROP KEY idx_album_photos_enabled_created,
      ADD KEY idx_album_photos_enabled_created (enabled, created_at);
  END IF;

  -- v0.1.0 inserts an unconfirmed upload disabled and leaves it untouched
  -- until it is confirmed, so a disabled row whose updated_at differs from
  -- created_at was confirmed and then deleted. Left unmarked it would read as
  -- an upload awaiting confirmation, and confirming it would bring it back.
  -- updated_at is kept: the retention of a deleted row is counted from it.
  UPDATE calendar_event_attachments
  SET retired_at = updated_at, updated_at = updated_at
  WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

  UPDATE album_photos
  SET retired_at = updated_at, updated_at = updated_at
  WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

  -- A thumbnail is stored beside its photo rather than underneath it.
  UPDATE storage_objects
  SET storage_key = CONCAT(LEFT(storage_key, CHAR_LENGTH(storage_key) - 6), '.thumb'),
      updated_at = updated_at
  WHERE storage_key LIKE 'albums/%/thumb';

  -- ---------------------------------------------------------------
  -- storage_objects.ref_count
  -- ---------------------------------------------------------------
  -- The count is every row that points at the object through a foreign key,
  -- whatever its state; v0.1.0 counted only confirmed rows. The referrers are
  -- read from information_schema, the same way conformance checks the count.
  SET SESSION group_concat_max_len = 1048576;
  SET @tc_so_refs := (
    SELECT GROUP_CONCAT(
             CONCAT('SELECT `', k.COLUMN_NAME, '` AS so_id FROM `', k.TABLE_NAME,
                    '` WHERE `', k.COLUMN_NAME, '` IS NOT NULL')
             ORDER BY k.TABLE_NAME, k.COLUMN_NAME SEPARATOR ' UNION ALL ')
    FROM information_schema.KEY_COLUMN_USAGE k
    WHERE k.TABLE_SCHEMA = DATABASE()
      AND k.REFERENCED_TABLE_SCHEMA = DATABASE()
      AND k.REFERENCED_TABLE_NAME = 'storage_objects'
      AND k.REFERENCED_COLUMN_NAME = 'id');
  SET @tc_so_sql := CONCAT(
    'UPDATE storage_objects so LEFT JOIN (SELECT so_id, COUNT(*) AS n FROM (', @tc_so_refs,
    ') refs GROUP BY so_id) r ON r.so_id = so.id ',
    'SET so.ref_count = IFNULL(r.n, 0), so.updated_at = so.updated_at ',
    'WHERE so.ref_count <> IFNULL(r.n, 0)');
  PREPARE tc_so_stmt FROM @tc_so_sql;
  EXECUTE tc_so_stmt;
  DEALLOCATE PREPARE tc_so_stmt;
  SET @tc_so_refs = NULL;
  SET @tc_so_sql = NULL;

  -- Same body as sql/time/views/storage_objects_unreferenced.sql, which
  -- scripts/check-upgrade-path.sh holds this to. A reservation is no longer
  -- listed: only the unconfirmed sweep reclaims one.
  CREATE OR REPLACE VIEW storage_objects_unreferenced AS
  SELECT so.id
  FROM storage_objects so
  WHERE so.ref_count = 0
    AND so.uploaded_at IS NOT NULL
    AND NOT EXISTS (
      SELECT 1 FROM calendar_event_attachments a WHERE a.storage_object_id = so.id
    )
    AND NOT EXISTS (
      SELECT 1 FROM album_photos ap WHERE ap.storage_object_id = so.id
    )
    AND NOT EXISTS (
      SELECT 1 FROM album_photos ap WHERE ap.thumbnail_object_id = so.id
    )
    AND NOT EXISTS (
      SELECT 1 FROM users u WHERE u.avatar_storage_object_id = so.id
    );

  -- ---------------------------------------------------------------
  -- sessions, users, sign-in allowlist
  -- ---------------------------------------------------------------
  -- A retired refresh token is recorded in the contract's rotated_from_hash;
  -- v0.1.0 kept a second copy of it in prev_refresh_hash.
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions'
      AND COLUMN_NAME = 'prev_refresh_hash'
  ) THEN
    UPDATE sessions SET rotated_from_hash = prev_refresh_hash, updated_at = updated_at
    WHERE prev_refresh_hash IS NOT NULL;
    ALTER TABLE sessions
      DROP KEY idx_sessions_prev_refresh_hash,
      DROP COLUMN prev_refresh_hash;
  END IF;

  -- Every existing session was issued by this application.
  UPDATE sessions SET audience = 'time', updated_at = updated_at WHERE audience = '';

  -- Addresses are stored and compared lower-case. The column's collation
  -- already refuses two addresses differing only in case, so none collide.
  UPDATE users SET email = LOWER(email), updated_at = updated_at
  WHERE CAST(email AS BINARY) <> CAST(LOWER(email) AS BINARY);

  -- Allowed addresses move to the shared allowlist. Any enabled row there
  -- turns the sign-in restriction on, even with no domain configured.
  IF EXISTS (
    SELECT 1 FROM information_schema.TABLES
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'oauth_allowed_emails'
  ) THEN
    INSERT INTO oauth_signin_allowlist
      (public_id, added_by_user_id, entry_kind, entry_value, sort_weight, notes, enabled, created_at)
    SELECT public_id, created_by_user_id, 'email', LOWER(email), sort_weight,
           COALESCE(NULLIF(reason, ''), notes), enabled, created_at
    FROM oauth_allowed_emails
    ON DUPLICATE KEY UPDATE oauth_signin_allowlist.id = oauth_signin_allowlist.id;
    DROP TABLE oauth_allowed_emails;
  END IF;

  -- ---------------------------------------------------------------
  -- calendar_events.recurrence_rule
  -- ---------------------------------------------------------------
  -- This application expanded weekly rules from Sunday; core reads a missing
  -- wkst as Monday, which would move the dates of an every-other-week rule.
  UPDATE calendar_events
  SET recurrence_rule = JSON_SET(recurrence_rule, '$.wkst', 'SU'), updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
    AND recurrence_rule->>'$.freq' = 'weekly'
    AND NOT JSON_CONTAINS_PATH(recurrence_rule, 'one', '$.wkst');

  -- ---------------------------------------------------------------
  -- events: kinds, payload keys, subject
  -- ---------------------------------------------------------------
  -- Kinds named differently in the core registry. An attendee change is an
  -- answer when it carries rsvp, an edit grant otherwise.
  UPDATE events SET type = 'calendar.member.added', updated_at = updated_at
  WHERE type = 'calendar.member.joined';
  UPDATE events
  SET type = 'calendar.member.removed', payload_json = JSON_SET(payload_json, '$.reason', 'left'),
      updated_at = updated_at
  WHERE type = 'calendar.member.left';
  UPDATE events
  SET type = CASE type
        WHEN 'calendar.comment.added'      THEN 'calendar.event.comment.created'
        WHEN 'calendar.comment.edited'     THEN 'calendar.event.comment.updated'
        WHEN 'calendar.comment.removed'    THEN 'calendar.event.comment.deleted'
        WHEN 'calendar.checklist.added'    THEN 'calendar.event.checklist.created'
        WHEN 'calendar.checklist.updated'  THEN 'calendar.event.checklist.updated'
        WHEN 'calendar.checklist.removed'  THEN 'calendar.event.checklist.deleted'
        WHEN 'calendar.attachment.added'   THEN 'calendar.event.attachment.created'
        WHEN 'calendar.attachment.removed' THEN 'calendar.event.attachment.deleted'
      END,
      updated_at = updated_at
  WHERE type IN ('calendar.comment.added', 'calendar.comment.edited', 'calendar.comment.removed',
                 'calendar.checklist.added', 'calendar.checklist.updated', 'calendar.checklist.removed',
                 'calendar.attachment.added', 'calendar.attachment.removed');
  UPDATE events
  SET type = IF(JSON_CONTAINS_PATH(payload_json, 'one', '$.rsvp'),
                'calendar.event.rsvp.updated', 'calendar.event.attendee.updated'),
      updated_at = updated_at
  WHERE type = 'calendar.attendee.updated';

  -- Comment rows no longer carry the comment text.
  UPDATE events
  SET payload_json = JSON_REMOVE(payload_json, '$.summary'), updated_at = updated_at
  WHERE type LIKE 'calendar.event.comment.%'
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.summary');

  -- What a detail row's summary held moves under the name core uses for it.
  UPDATE events
  SET payload_json = JSON_REMOVE(
        JSON_SET(payload_json,
                 IF(type LIKE 'calendar.event.attachment.%', '$.filename', '$.itemTitle'),
                 payload_json->'$.summary'),
        '$.summary'),
      updated_at = updated_at
  WHERE (type LIKE 'calendar.event.attachment.%' OR type LIKE 'calendar.event.checklist.%')
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.summary');

  -- On the event itself and on an attendee change, summary is the title.
  UPDATE events
  SET payload_json = JSON_REMOVE(JSON_SET(payload_json, '$.title', payload_json->'$.summary'), '$.summary'),
      updated_at = updated_at
  WHERE type IN ('calendar.event.created', 'calendar.event.updated', 'calendar.event.deleted',
                 'calendar.event.rsvp.updated', 'calendar.event.attendee.updated')
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.summary');

  -- id named the event on every calendar.event.* row, and event repeated it.
  UPDATE events
  SET payload_json = JSON_REMOVE(
        JSON_SET(payload_json, '$.eventId', COALESCE(payload_json->'$.id', payload_json->'$.event')),
        '$.id', '$.event'),
      updated_at = updated_at
  WHERE type LIKE 'calendar.event.%'
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.id', '$.event');

  -- The detail's own id was carried as subject, and the attendee as user.
  UPDATE events
  SET payload_json = JSON_REMOVE(
        JSON_SET(payload_json,
                 CASE
                   WHEN type LIKE 'calendar.event.comment.%'   THEN '$.commentId'
                   WHEN type LIKE 'calendar.event.checklist.%' THEN '$.itemId'
                   ELSE '$.attachmentId'
                 END,
                 payload_json->'$.subject'),
        '$.subject'),
      updated_at = updated_at
  WHERE (type LIKE 'calendar.event.comment.%' OR type LIKE 'calendar.event.checklist.%'
         OR type LIKE 'calendar.event.attachment.%')
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.subject');
  UPDATE events
  SET payload_json = JSON_REMOVE(JSON_SET(payload_json, '$.userId', payload_json->'$.user'), '$.user'),
      updated_at = updated_at
  WHERE type IN ('calendar.event.rsvp.updated', 'calendar.event.attendee.updated')
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.user');

  -- A detail row names the event's title, read from the event as it is now.
  UPDATE events e
  JOIN calendar_events ce ON ce.public_id = e.subject_public_id
  SET e.payload_json = JSON_SET(e.payload_json, '$.title', ce.title), e.updated_at = e.updated_at
  WHERE e.type LIKE 'calendar.event.%'
    AND NOT JSON_CONTAINS_PATH(e.payload_json, 'one', '$.title');

  -- id becomes the entity key the registry names for each other kind.
  UPDATE events
  SET payload_json = JSON_REMOVE(
        JSON_SET(payload_json,
                 CASE
                   WHEN type LIKE 'calendar.memo.%'   THEN '$.memoId'
                   WHEN type LIKE 'calendar.photo.%'  THEN '$.photoId'
                   WHEN type LIKE 'calendar.invite.%' THEN '$.inviteId'
                   WHEN type LIKE 'calendar.member.%' THEN '$.userId'
                   ELSE '$.calendarId'
                 END,
                 payload_json->'$.id'),
        '$.id'),
      updated_at = updated_at
  WHERE (type LIKE 'calendar.memo.%' OR type LIKE 'calendar.photo.%'
         OR type LIKE 'calendar.invite.%' OR type LIKE 'calendar.member.%'
         OR type IN ('calendar.created', 'calendar.updated', 'calendar.deleted'))
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.id');

  -- Memo and membership rows name their calendar, as the registry requires.
  UPDATE events e
  JOIN calendars c ON c.id = e.calendar_id
  SET e.payload_json = JSON_SET(e.payload_json, '$.calendarId', BIN_TO_UUID(c.public_id)),
      e.updated_at = e.updated_at
  WHERE (e.type LIKE 'calendar.memo.%' OR e.type LIKE 'calendar.member.%')
    AND NOT JSON_CONTAINS_PATH(e.payload_json, 'one', '$.calendarId');

  -- Core 0001 filled subject_public_id from $.id on every row; only the kinds
  -- the registries give a subject keep it.
  UPDATE events
  SET subject_public_id = NULL, updated_at = updated_at
  WHERE subject_public_id IS NOT NULL
    AND (type LIKE 'calendar.%' OR type LIKE 'workspace.%')
    AND type NOT LIKE 'calendar.event.%'
    AND type NOT LIKE 'calendar.memo.%'
    AND type NOT LIKE 'calendar.photo.%'
    AND type NOT LIKE 'calendar.invite.%';

  -- ---------------------------------------------------------------
  -- workspace membership behind every calendar membership
  -- ---------------------------------------------------------------
  -- A member removed from the workspace keeps no calendar access; nothing
  -- revives a removed workspace membership automatically.
  UPDATE calendar_members cm
  JOIN calendars c ON c.id = cm.calendar_id
  JOIN workspace_members wm ON wm.workspace_id = c.workspace_id AND wm.user_id = cm.user_id
  SET cm.enabled = FALSE, cm.updated_at = cm.updated_at
  WHERE cm.enabled = TRUE AND wm.enabled = FALSE;

  -- This application never required workspace membership, so a calendar
  -- member with no row at all is recorded as the member it already was. The
  -- public_id is a UUID v7: millisecond time, version 7, variant 10, random.
  INSERT INTO workspace_members (public_id, workspace_id, user_id, role, joined_at, enabled)
  SELECT UNHEX(CONCAT(
           LPAD(HEX(FLOOR(UNIX_TIMESTAMP(NOW(3)) * 1000)), 12, '0'),
           '7', SUBSTR(HEX(RANDOM_BYTES(2)), 1, 3),
           HEX(8 | (ASCII(RANDOM_BYTES(1)) & 3)), SUBSTR(HEX(RANDOM_BYTES(8)), 1, 15))),
         m.workspace_id, m.user_id, 'member', m.joined_at, TRUE
  FROM (
    SELECT c.workspace_id, cm.user_id, MIN(cm.created_at) AS joined_at
    FROM calendar_members cm
    JOIN calendars c ON c.id = cm.calendar_id
    WHERE cm.enabled = TRUE
      AND NOT EXISTS (
        SELECT 1 FROM workspace_members wm
        WHERE wm.workspace_id = c.workspace_id AND wm.user_id = cm.user_id)
    GROUP BY c.workspace_id, cm.user_id
  ) m;
END//

DELIMITER ;

CALL upgrade_from_v0_1_0();

DROP PROCEDURE upgrade_from_v0_1_0;
