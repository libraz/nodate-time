-- ====================================
-- calendars
-- Calendar containers. Who may reach one is calendar_members; what a
-- viewer is allowed to see inside it is the event's own visibility.
--
-- There is deliberately no 'shared' kind. `kind` says where a
-- calendar's contents come from, and sharing is not a source: the
-- number of people who can reach a calendar is a count of
-- calendar_members rows, and encoding it a second time in an enum
-- gives the two encodings a way to disagree. What does change with
-- sharing is owner_user_id — see the column comment.
-- ====================================
CREATE TABLE calendars (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',

  kind ENUM('personal','system') NOT NULL DEFAULT 'personal' COMMENT 'Where the contents come from: personal (written by people through the API) or system (populated from a provider feed identified by system_slug, and read-only to users). Not an audience: see the table comment.',
  name VARCHAR(255) NOT NULL COMMENT 'Display name',
  description TEXT NULL COMMENT 'Optional description',
  color VARCHAR(7) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '#4285F4' COMMENT 'Default hex color',
  cover_url VARCHAR(2048) NULL COMMENT 'Cover image URL',

  owner_user_id INT UNSIGNED NULL COMMENT 'The user this calendar belongs to, or NULL when it belongs to no one in particular: a system feed, or a calendar shared by a group that outlives any single member. NULL is not cosmetic — the FK cascades, so naming an owner means deleting that user deletes the calendar and every event in it. A group calendar must leave this NULL or one departure takes everyone else''s history with it.',
  system_slug VARCHAR(100) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'For system calendars: provider identifier (e.g., holidays.jp)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendars_public_id (public_id),
  UNIQUE KEY uniq_calendars_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendars_system_slug (workspace_id, system_slug),
  KEY idx_calendars_workspace_id_kind (workspace_id, kind),

  CONSTRAINT fk_calendars_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendars_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar containers. Access is calendar_members; kind is the source of the contents, not the audience. owner_user_id NULL means the calendar outlives any single member.';
