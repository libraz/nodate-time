-- Brings a core schema built from an older contract up to the columns,
-- indexes and tables the current one declares, keeping its data. Covers
-- every core DDL change since the vendored pin 6522b056, including the
-- columns a product layer used to add with its own ALTERs.
--
-- Three starting states are expected: a fresh core (nothing to do), a
-- nodate-time v0.1.0 schema, and a nodate-time schema built with its
-- product-layer ALTERs. Every step inspects information_schema first,
-- judging a changed column by its attributes rather than by its presence,
-- so the file can be run again and an interrupted run resumes.
--
-- Stop every application first. Apply the core upgrades in number order,
-- then each product layer's upgrade, then `run.sh --mode schema`.

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS oauth_signin_allowlist (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  added_by_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id (who added the entry, null once that account is gone)',

  entry_kind ENUM('domain','email') NOT NULL COMMENT 'What entry_value names: domain matches the part after the final ''@'', email matches the whole address',
  entry_value VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_bin NOT NULL COMMENT 'The domain or address this entry admits, normalized to lower case before it is written; latin1_bin keeps every comparison byte-exact, so cafe.example and café.example are distinct entries rather than one',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag — FALSE withdraws the entry without releasing its (entry_kind, entry_value) claim, so re-adding it revives this row via ON DUPLICATE KEY UPDATE',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_oauth_signin_allowlist_public_id (public_id),
  UNIQUE KEY uniq_oauth_signin_allowlist_kind_value (entry_kind, entry_value),

  CONSTRAINT fk_oauth_signin_allowlist_added_by
    FOREIGN KEY (added_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Instance-level OAuth/OIDC sign-in allowlist';

CREATE TABLE IF NOT EXISTS storage_blob_removals (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  storage_key VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Object key whose blob is still to be removed from object storage',

  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_storage_blob_removals_storage_key (storage_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Pending object-storage blob removals whose referencing rows are already deleted';
DROP PROCEDURE IF EXISTS core_upgrade_0001;

DELIMITER //

CREATE PROCEDURE core_upgrade_0001()
BEGIN
  -- ---------------------------------------------------------------
  -- calendar_events
  -- ---------------------------------------------------------------
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND INDEX_NAME = 'idx_calendar_events_reminder_scan'
  ) THEN
    ALTER TABLE calendar_events
      ADD KEY idx_calendar_events_reminder_scan (enabled, notification_offset, start_at);
  END IF;

  -- latin1 refuses an internationalised address outright; the column
  -- is judged by its charset, since it exists in every starting state.
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND COLUMN_NAME = 'url'
      AND (CHARACTER_SET_NAME <> 'utf8mb4' OR COLLATION_NAME <> 'utf8mb4_0900_ai_ci')
  ) THEN
    ALTER TABLE calendar_events
      MODIFY COLUMN url VARCHAR(2048) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NULL
        COMMENT 'Meeting link or related URL. utf8mb4 rather than latin1: an internationalised address (RFC 3987) carries non-ASCII directly, and latin1 refuses the whole value.';
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND COLUMN_NAME = 'source_uid'
  ) THEN
    ALTER TABLE calendar_events
      ADD COLUMN source_uid VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NULL
        COMMENT 'UID of the VEVENT this event was imported from, as the file gave it. NULL when the event was created here, when it is a recurrence override, or when the file UID exceeded the column.';
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND COLUMN_NAME = 'source_uid_key'
  ) THEN
    ALTER TABLE calendar_events
      ADD COLUMN source_uid_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs
        GENERATED ALWAYS AS (IF(enabled, source_uid, NULL)) STORED
        COMMENT 'source_uid while the row is live, NULL once it is soft-deleted. Exists solely to scope uniq_calendar_events_source_uid to rows the calendar still shows. Generated: never written directly.';
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND INDEX_NAME = 'uniq_calendar_events_source_uid'
  ) THEN
    ALTER TABLE calendar_events
      ADD UNIQUE KEY uniq_calendar_events_source_uid (calendar_id, source_uid_key);
  END IF;

  -- ---------------------------------------------------------------
  -- calendar_event_attachments, calendar_event_comments
  -- ---------------------------------------------------------------
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
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_event_comments'
      AND INDEX_NAME = 'idx_calendar_event_comments_thread'
  ) THEN
    ALTER TABLE calendar_event_comments
      ADD KEY idx_calendar_event_comments_thread (workspace_id, event_id, enabled, deleted_at, created_at, id);
  END IF;

  -- ---------------------------------------------------------------
  -- events
  -- ---------------------------------------------------------------
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND INDEX_NAME = 'idx_events_workspace_calendar_id'
  ) THEN
    ALTER TABLE events
      ADD KEY idx_events_workspace_calendar_id (workspace_id, calendar_id, id);
  END IF;

  -- subject_public_id becomes a plain BINARY(16) the appender writes.
  -- A generated VARCHAR(36) read from payload_json is rebuilt beside it
  -- as subject_public_id_bin, filled, then swapped in; a leftover _bin
  -- column means an earlier run stopped part-way, and the steps below
  -- pick up from wherever it stopped.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND COLUMN_NAME IN ('subject_public_id', 'subject_public_id_bin')
  ) THEN
    ALTER TABLE events
      ADD COLUMN subject_public_id BINARY(16) NULL
        COMMENT 'public_id of the entity this event is about, so one entity history is an index range. Set for every kind whose event-kinds registry entry names a subject table, NULL for the rest. Written by the appender alongside the payload; never derived from it.'
        AFTER reverses_event_id;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND COLUMN_NAME = 'subject_public_id'
      AND (COLUMN_TYPE <> 'binary(16)' OR GENERATION_EXPRESSION <> '')
  ) THEN
    IF NOT EXISTS (
      SELECT 1 FROM information_schema.COLUMNS
      WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
        AND COLUMN_NAME = 'subject_public_id_bin'
    ) THEN
      ALTER TABLE events
        ADD COLUMN subject_public_id_bin BINARY(16) NULL
          COMMENT 'public_id of the entity this event is about, so one entity history is an index range. Set for every kind whose event-kinds registry entry names a subject table, NULL for the rest. Written by the appender alongside the payload; never derived from it.'
          AFTER reverses_event_id;
    END IF;

    -- updated_at = updated_at keeps the log rows' timestamps as written.
    UPDATE events
    SET subject_public_id_bin = IF(IS_UUID(subject_public_id), UUID_TO_BIN(subject_public_id), NULL),
        updated_at = updated_at;

    -- Dropped explicitly: dropping the column alone would leave the key
    -- behind as (workspace_id, calendar_id, id) under the subject's name.
    IF EXISTS (
      SELECT 1 FROM information_schema.STATISTICS
      WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
        AND INDEX_NAME = 'idx_events_workspace_calendar_subject'
    ) THEN
      ALTER TABLE events DROP KEY idx_events_workspace_calendar_subject;
    END IF;

    ALTER TABLE events DROP COLUMN subject_public_id;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND COLUMN_NAME = 'subject_public_id_bin'
  ) THEN
    ALTER TABLE events RENAME COLUMN subject_public_id_bin TO subject_public_id;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'events'
      AND INDEX_NAME = 'idx_events_workspace_calendar_subject'
  ) THEN
    ALTER TABLE events
      ADD KEY idx_events_workspace_calendar_subject (workspace_id, calendar_id, subject_public_id, id);
  END IF;

  -- ---------------------------------------------------------------
  -- identities
  -- ---------------------------------------------------------------
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'identities'
      AND COLUMN_NAME = 'provider'
      AND COLUMN_TYPE NOT LIKE '%''line''%'
  ) THEN
    ALTER TABLE identities
      MODIFY COLUMN provider ENUM('local','google','github','microsoft','generic_oidc','line')
        NOT NULL COMMENT 'Identity provider kind';
  END IF;

  -- ---------------------------------------------------------------
  -- sessions
  -- ---------------------------------------------------------------
  -- Existing rows get ''. The product layer that owns them fills in its
  -- own name in its upgrade; conformance refuses a row left at ''.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions'
      AND COLUMN_NAME = 'audience'
  ) THEN
    ALTER TABLE sessions
      ADD COLUMN audience VARCHAR(32) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT ''
        COMMENT 'Name of the application that issued the session (flow, time), written as a SQL literal. An application refreshes, looks up and purges only its own rows; revoking every session of a user ignores it. Empty only on a row no conforming writer produced.'
        AFTER user_id;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions'
      AND COLUMN_NAME = 'rotated_at'
  ) THEN
    ALTER TABLE sessions
      ADD COLUMN rotated_at DATETIME(3) NULL
        COMMENT 'When the refresh token was last exchanged. Lets an application tell a replay of rotated_from_hash from one client asking twice within a grace window.'
        AFTER rotated_from_hash;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'sessions'
      AND INDEX_NAME = 'idx_sessions_expires_at'
  ) THEN
    ALTER TABLE sessions
      ADD KEY idx_sessions_expires_at (expires_at);
  END IF;
END//

DELIMITER ;

CALL core_upgrade_0001();

DROP PROCEDURE core_upgrade_0001;
