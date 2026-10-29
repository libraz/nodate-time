-- Upgrades a database created by v0.1.0 to the current schema, keeping its data.
--
-- Stop the v0.1.0 API first, apply this, then start the new one. The new code
-- reads the columns and the view added here, and the rows v0.1.0 deleted are
-- told apart from unconfirmed uploads by a marking that only a stopped v0.1.0
-- cannot outdate: a row it deletes after the marking reads as an upload still
-- awaiting confirmation. The only column dropped is sessions.prev_refresh_hash,
-- after its values are carried into the contract's rotated_from_hash, and the
-- only rows rewritten besides the deletion marking are comment log entries,
-- which lose their text, and thumbnail storage keys, which move from
-- <photo>/thumb to <photo>.thumb. The objects themselves are renamed when they
-- are copied to the new object store; see the README.
--
-- Every step checks what is already there, so a run that was interrupted can be
-- run again, and so can one applied too early: run it once more after the old
-- API has stopped and before the new one starts. Once the new API has served a
-- request, do not run it again -- confirming an upload also touches updated_at,
-- which is what the marking reads.

-- updated_at is a TIMESTAMP, shown in the session's zone, and created_at is a
-- DATETIME the API wrote in UTC. The marking below compares them.
SET time_zone = '+00:00';

DROP PROCEDURE IF EXISTS upgrade_from_v0_1_0;

DELIMITER //

CREATE PROCEDURE upgrade_from_v0_1_0()
BEGIN
  -- A deleted attachment or photo is now told apart from an upload that was
  -- never confirmed: both have enabled = FALSE, and retired_at is set only on
  -- the deleted one.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_event_attachments'
      AND COLUMN_NAME = 'retired_at'
  ) THEN
    ALTER TABLE calendar_event_attachments
      ADD COLUMN retired_at DATETIME(3) NULL
        COMMENT 'When a confirmed attachment was deleted. NULL on a live row and on a reservation that was never confirmed.'
        AFTER enabled;
  END IF;

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

  -- Mark the rows v0.1.0 deleted. In v0.1.0 an unconfirmed upload is inserted
  -- disabled and never updated until it is confirmed, so a disabled row whose
  -- updated_at differs from created_at was confirmed and then deleted. Left
  -- unmarked, a deleted row would read as an upload still awaiting
  -- confirmation, and confirming it would bring it back. updated_at is kept:
  -- the retention of a deleted row is counted from it.
  UPDATE calendar_event_attachments
  SET retired_at = updated_at, updated_at = updated_at
  WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

  UPDATE album_photos
  SET retired_at = updated_at, updated_at = updated_at
  WHERE enabled = FALSE AND retired_at IS NULL AND updated_at <> created_at;

  -- Lets the activity feed page one calendar's log by id.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND INDEX_NAME = 'idx_events_workspace_calendar_id'
  ) THEN
    ALTER TABLE events
      ADD KEY idx_events_workspace_calendar_id (workspace_id, calendar_id, id);
  END IF;

  -- A payload whose id is not a 36-character UUID yields NULL here instead of
  -- failing the insert. The expression is the only thing that changes; the
  -- index on the column is rebuilt with it.
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND COLUMN_NAME = 'subject_public_id'
      AND LOWER(GENERATION_EXPRESSION) NOT LIKE '%regexp%'
  ) THEN
    ALTER TABLE events
      MODIFY COLUMN subject_public_id VARCHAR(36)
        CHARACTER SET latin1 COLLATE latin1_swedish_ci
        GENERATED ALWAYS AS (
          IF(CHAR_LENGTH(payload_json->>'$.id') = 36
               AND payload_json->>'$.id' REGEXP '^[0-9a-fA-F-]{36}$',
             payload_json->>'$.id', NULL)
        ) STORED
        COMMENT 'Public id of the entity this event is about, lifted out of the payload so one entity history can be indexed. Generated: never written directly.';
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

  -- A retired refresh token is recorded in the contract's rotated_from_hash;
  -- v0.1.0 kept a second copy of it in prev_refresh_hash.
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions'
      AND COLUMN_NAME = 'prev_refresh_hash'
  ) THEN
    UPDATE sessions SET rotated_from_hash = prev_refresh_hash
    WHERE prev_refresh_hash IS NOT NULL;
    ALTER TABLE sessions
      DROP KEY idx_sessions_prev_refresh_hash,
      DROP COLUMN prev_refresh_hash;
  END IF;

  -- Comment log rows no longer carry the comment text; drop it from old ones.
  UPDATE events
  SET payload_json = JSON_REMOVE(payload_json, '$.summary'), updated_at = updated_at
  WHERE type LIKE 'calendar.comment.%'
    AND JSON_CONTAINS_PATH(payload_json, 'one', '$.summary');

  -- A thumbnail is stored beside its photo rather than underneath it.
  UPDATE storage_objects
  SET storage_key = CONCAT(LEFT(storage_key, CHAR_LENGTH(storage_key) - 6), '.thumb'),
      updated_at = updated_at
  WHERE storage_key LIKE 'albums/%/thumb';

  -- The objects nothing refers to; the storage sweep deletes only these.
  CREATE OR REPLACE VIEW storage_objects_unreferenced AS
  SELECT so.id
  FROM storage_objects so
  WHERE so.ref_count = 0
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
END//

DELIMITER ;

CALL upgrade_from_v0_1_0();

DROP PROCEDURE upgrade_from_v0_1_0;
