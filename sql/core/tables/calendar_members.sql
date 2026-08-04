-- ====================================
-- calendar_members
-- Who may see a calendar, and what they may do with it.
--
-- This is an ACL axis, unlike calendar_subscriptions, which holds one
-- subscriber's private display preferences and deliberately grants
-- nothing. Two tables because the questions are different: membership is
-- shared state that the calendar's owner controls, a subscription is
-- personal state its own user controls, and letting one row answer both
-- means changing your sidebar colour is a permission write.
--
-- Workspace membership is a prerequisite, not a substitute. A workspace
-- may hold several calendars whose audiences do not coincide, so being in
-- the workspace says a member could be given access, and a row here says
-- they were.
--
-- Roles are ordered: owner > manager > editor > viewer. Read them as
--   owner    controls membership and can delete the calendar
--   manager  controls membership, cannot delete the calendar
--   editor   writes events
--   viewer   reads
-- Callers must resolve through the calendar resolution helpers rather
-- than reading this table directly, because the lookup and the role check
-- belong together — separating them is how an authorization check gets
-- skipped without anyone noticing.
-- ====================================
CREATE TABLE calendar_members (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  role ENUM('owner','manager','editor','viewer') NOT NULL DEFAULT 'viewer' COMMENT 'What this member may do with the calendar. Defaults to the least privilege so a writer that omits it cannot accidentally grant access.',

  -- The colour everyone sees for this member's events, as distinct from
  -- calendar_subscriptions.display_color, which is one subscriber's
  -- private override for a whole calendar. On a shared calendar the point
  -- is telling people apart, which only works if the assignment is agreed
  -- rather than per-viewer.
  member_color VARCHAR(7) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '#4285F4' COMMENT 'Shared colour identifying this member on the calendar; agreed rather than per-viewer, unlike calendar_subscriptions.display_color',

  invited_by_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id; who granted this membership. NULL for the owner row created with the calendar.',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Soft-delete flag; FALSE means the membership was revoked. Every access check must filter on enabled = TRUE.',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_members_public_id (public_id),
  UNIQUE KEY uniq_calendar_members_workspace_public_id (workspace_id, public_id),
  -- One row per (calendar, user) regardless of enabled, so re-adding a
  -- removed member updates the existing row rather than accumulating
  -- revoked duplicates that a future access check might read.
  UNIQUE KEY uniq_calendar_members_calendar_user (calendar_id, user_id),
  KEY idx_calendar_members_user_workspace (user_id, workspace_id, enabled),
  KEY idx_calendar_members_calendar_role (calendar_id, role, enabled),

  CONSTRAINT fk_calendar_members_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_members_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_members_invited_by FOREIGN KEY (invited_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-calendar access grants: which users may use a calendar and at what role. The ACL axis, unlike calendar_subscriptions, which holds private display preferences and grants nothing.';
