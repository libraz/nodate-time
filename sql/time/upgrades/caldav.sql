-- CalDAV app passwords and the event resource-name table.
-- Idempotent; apply after calendar-covers.sql and before triggers.sql.
-- A table is skipped when it already exists, so compare a hand-made one
-- with its file under sql/time/tables/ first.

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS dav_app_passwords (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  name VARCHAR(100) NOT NULL COMMENT 'Label chosen by the user, usually the device',
  token_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the password. The plaintext is shown once at creation and never stored.',
  token_prefix CHAR(8) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Leading characters of the plaintext, shown in lists so a user can tell passwords apart',
  last_used_at DATETIME(3) NULL COMMENT 'Last successful authentication; NULL = never used',
  revoked_at DATETIME(3) NULL COMMENT 'Revocation time; a non-NULL value makes the password unusable',

  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_dav_app_passwords_public_id (public_id),
  UNIQUE KEY uniq_dav_app_passwords_token_hash (token_hash),
  KEY idx_dav_app_passwords_user (workspace_id, user_id),

  CONSTRAINT fk_dav_app_passwords_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_dav_app_passwords_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-device CalDAV passwords';

CREATE TABLE IF NOT EXISTS dav_event_resources (
  event_id INT UNSIGNED NOT NULL PRIMARY KEY COMMENT 'Internal FK to calendar_events.id',
  href_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Resource name chosen by the client; compared case-sensitively',
  uid VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'iCalendar UID chosen by the client; compared case-sensitively',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  KEY idx_dav_event_resources_href_name (href_name),
  KEY idx_dav_event_resources_uid (uid),

  CONSTRAINT fk_dav_event_resources_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CalDAV resource name and UID per event';
