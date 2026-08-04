-- ====================================
-- users
-- Account-level principals. Authenticated via identities (local/oidc).
-- Not workspace-scoped; users are global and join workspaces via workspace_members.
-- ====================================
CREATE TABLE users (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',

  email VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Primary email, ASCII only',
  email_verified_at DATETIME(3) NULL COMMENT 'Email verification timestamp',
  display_name VARCHAR(255) NOT NULL COMMENT 'Human-readable name',
  avatar_url VARCHAR(2048) NULL COMMENT 'Avatar image URL; used when the avatar is hosted externally (e.g. OIDC provider)',
  avatar_storage_object_id INT UNSIGNED NULL COMMENT 'FK to storage_objects.id when the user uploaded their own avatar; NULL when avatar_url (external) is used or no avatar is set',
  locale VARCHAR(16) NOT NULL DEFAULT 'en' COMMENT 'Preferred locale tag (BCP 47)',
  timezone VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT 'UTC' COMMENT 'Preferred IANA timezone (independent of locale)',
  country CHAR(2) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'ISO 3166-1 alpha-2 country (independent of locale); drives default holiday subscription',
  week_start ENUM('mon','sun','sat') NOT NULL DEFAULT 'mon' COMMENT 'Preferred first day of the week for calendar grids',
  working_days CHAR(7) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'User override of workspace working_days; NULL = inherit',
  working_hours_start TIME NULL COMMENT 'User override of workspace working_hours_start; NULL = inherit',
  working_hours_end TIME NULL COMMENT 'User override of workspace working_hours_end; NULL = inherit',
  snap_to_working_day ENUM('off','warn','auto') NOT NULL DEFAULT 'warn' COMMENT 'What happens when a task/event lands on a non-working day: off=accept silently, warn=save with badge, auto=itemkit snaps forward to next working day',
  treat_holidays_as_non_working BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'If true, subscribed system (holiday) calendar events count as non-working days',
  theme_preference ENUM('aurora-light','aurora-dark','dotline-light','dotline-dark','glass-light','glass-dark','system') NOT NULL DEFAULT 'system' COMMENT 'UI theme preference',
  calendar_shift_default ENUM('ask','sync_always','task_only_always') NOT NULL DEFAULT 'ask' COMMENT 'Default behaviour when an event linked to safe tasks is shifted: ask=prompt the user every time (current behaviour), sync_always=also shift every linked safe task by the same delta, task_only_always=shift only the event and leave linked tasks alone',
  last_login_at DATETIME(3) NULL COMMENT 'Last successful login',

  -- Notification channel toggles (see /settings/notifications).
  notif_email_digest_enabled     BOOLEAN NOT NULL DEFAULT TRUE  COMMENT 'Weekly digest email',
  notif_email_mention_enabled    BOOLEAN NOT NULL DEFAULT TRUE  COMMENT 'Email when mentioned in comments',
  notif_email_assignment_enabled BOOLEAN NOT NULL DEFAULT TRUE  COMMENT 'Email when assigned to a task',
  notif_email_due_soon_enabled   BOOLEAN NOT NULL DEFAULT TRUE  COMMENT 'Email when owned task is due within 24h',
  notif_web_push_enabled         BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Browser push notifications',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_users_public_id (public_id),
  UNIQUE KEY uniq_users_email (email),
  KEY idx_users_enabled (enabled),
  KEY idx_users_avatar_storage_object (avatar_storage_object_id),

  -- SET NULL: if the underlying storage_objects row is removed (e.g. after
  -- ref_count reaches 0 via a sweeper) the user simply loses their avatar
  -- rather than blocking the deletion.
  CONSTRAINT fk_users_avatar_storage_object FOREIGN KEY (avatar_storage_object_id) REFERENCES storage_objects(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Global user accounts';
