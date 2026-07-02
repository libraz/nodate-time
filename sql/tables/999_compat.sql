-- Idempotent compatibility alters for existing local development databases.
-- The table definitions above initialize fresh databases; these statements
-- bring already-created compose volumes up to the current schema.
SET @add_token_version = (
  SELECT IF(
    COUNT(*) = 0,
    'ALTER TABLE users ADD COLUMN token_version INT UNSIGNED NOT NULL DEFAULT 1 AFTER password_hash',
    'SELECT 1'
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
    'SELECT 1'
  )
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'users'
    AND column_name = 'password_changed_at'
);
PREPARE stmt FROM @add_password_changed_at;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
