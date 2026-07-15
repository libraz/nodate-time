-- Idempotent compatibility alters for databases created by older releases.
-- The table definitions above initialize fresh databases; these statements
-- bring existing compose volumes up to the current schema. Keep every column,
-- index, and foreign key that was added after a table's initial release here.
SET @add_avatar_storage_key = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN avatar_storage_key VARCHAR(1000) NULL AFTER color',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'avatar_storage_key'
);
PREPARE stmt FROM @add_avatar_storage_key;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_avatar_content_type = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN avatar_content_type VARCHAR(255) NULL AFTER avatar_storage_key',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'avatar_content_type'
);
PREPARE stmt FROM @add_avatar_content_type;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_token_version = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN token_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER password_hash',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'token_version'
);
PREPARE stmt FROM @add_token_version;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_password_changed_at = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN password_changed_at DATETIME(3) NULL AFTER token_version',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'password_changed_at'
);
PREPARE stmt FROM @add_password_changed_at;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_is_admin = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE AFTER password_changed_at',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'is_admin'
);
PREPARE stmt FROM @add_is_admin;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_event_timezone = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD COLUMN timezone VARCHAR(64) NOT NULL DEFAULT ''UTC'' COMMENT ''IANA timezone name (e.g. Asia/Tokyo)'' AFTER end_at',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'events'
    AND column_name = 'timezone'
);
PREPARE stmt FROM @add_event_timezone;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_recurrence_parent_id = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD COLUMN recurrence_parent_id INT UNSIGNED NULL COMMENT ''master recurring event for an exception'' AFTER recurrence_end',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'events'
    AND column_name = 'recurrence_parent_id'
);
PREPARE stmt FROM @add_recurrence_parent_id;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_recurrence_original_start = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD COLUMN recurrence_original_start DATETIME(3) NULL COMMENT ''original occurrence start in UTC'' AFTER recurrence_parent_id',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'events'
    AND column_name = 'recurrence_original_start'
);
PREPARE stmt FROM @add_recurrence_original_start;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_recurrence_cancelled = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD COLUMN recurrence_cancelled TINYINT(1) NOT NULL DEFAULT 0 COMMENT ''single-occurrence tombstone'' AFTER recurrence_original_start',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'events'
    AND column_name = 'recurrence_cancelled'
);
PREPARE stmt FROM @add_recurrence_cancelled;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_recurrence_exception_index = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD UNIQUE KEY uk_events_recurrence_exception (recurrence_parent_id, recurrence_original_start)',
    'DO 0'
  )
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'events'
    AND index_name = 'uk_events_recurrence_exception'
);
PREPARE stmt FROM @add_recurrence_exception_index;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_recurrence_parent_fk = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE events ADD CONSTRAINT fk_events_recurrence_parent FOREIGN KEY (recurrence_parent_id) REFERENCES events (id) ON DELETE CASCADE',
    'DO 0'
  )
  FROM information_schema.table_constraints
  WHERE constraint_schema = DATABASE()
    AND table_name = 'events'
    AND constraint_name = 'fk_events_recurrence_parent'
);
PREPARE stmt FROM @add_recurrence_parent_fk;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_memo_body = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE memos ADD COLUMN body TEXT NOT NULL AFTER title',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'memos'
    AND column_name = 'body'
);
PREPARE stmt FROM @add_memo_body;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_invite_is_public = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE calendar_invites ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE AFTER use_count',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'calendar_invites'
    AND column_name = 'is_public'
);
PREPARE stmt FROM @add_invite_is_public;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_oauth_code_verifier = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE oauth_states ADD COLUMN code_verifier VARCHAR(128) NOT NULL DEFAULT '''' AFTER redirect',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'oauth_states'
    AND column_name = 'code_verifier'
);
PREPARE stmt FROM @add_oauth_code_verifier;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

SET @add_oauth_nonce = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE oauth_states ADD COLUMN nonce VARCHAR(64) NOT NULL DEFAULT '''' AFTER code_verifier',
    'DO 0'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'oauth_states'
    AND column_name = 'nonce'
);
PREPARE stmt FROM @add_oauth_nonce;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
