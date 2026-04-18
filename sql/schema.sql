-- Auto-generated schema. Do not edit directly.
-- Run: bash sql/build-schema.sh

-- ===== tables/001_users.sql =====
CREATE TABLE IF NOT EXISTS users (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  name       VARCHAR(100) NOT NULL,
  email      VARCHAR(255) NOT NULL,
  icon       VARCHAR(10)  NOT NULL DEFAULT '👤',
  color      VARCHAR(7)   NOT NULL DEFAULT '#42A5F5',
  password_hash VARCHAR(255) NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_public_id (public_id),
  UNIQUE KEY uk_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/002_calendars.sql =====
CREATE TABLE IF NOT EXISTS calendars (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  name       VARCHAR(200) NOT NULL,
  color      VARCHAR(7)   NOT NULL DEFAULT '#4CAF50',
  cover_url  VARCHAR(500) NOT NULL DEFAULT '',
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_calendars_public_id (public_id),
  CONSTRAINT fk_calendars_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/003_calendar_members.sql =====
-- Calendar membership with role-based access.
-- role: 'admin' (full control) | 'member' (view + create events) | 'viewer' (read-only)
CREATE TABLE IF NOT EXISTS calendar_members (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id INT UNSIGNED NOT NULL,
  user_id     INT UNSIGNED NOT NULL,
  role        ENUM('admin', 'member', 'viewer') NOT NULL DEFAULT 'member',
  color       VARCHAR(7)   NOT NULL DEFAULT '#42A5F5' COMMENT 'member color within this calendar',
  joined_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_cal_member (calendar_id, user_id),
  CONSTRAINT fk_cm_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_cm_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/004_events.sql =====
CREATE TABLE IF NOT EXISTS events (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id   BINARY(16)   NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  title       VARCHAR(500) NOT NULL,
  all_day     TINYINT(1)   NOT NULL DEFAULT 0,
  start_at    DATETIME(3)  NOT NULL,
  end_at      DATETIME(3)  NOT NULL,
  color       VARCHAR(7)   NOT NULL DEFAULT '#42A5F5',
  location    VARCHAR(500) NOT NULL DEFAULT '',
  memo        TEXT         NOT NULL,
  url         VARCHAR(2000) NOT NULL DEFAULT '' COMMENT 'optional URL or meeting link',
  created_by  INT UNSIGNED NOT NULL,
  assigned_to         INT UNSIGNED NULL     COMMENT 'member responsible for this event',
  notification_offset INT          NULL     COMMENT 'minutes before event to notify; NULL = none',
  recurrence_rule JSON         NULL     COMMENT 'recurrence rule: {freq, interval, byDay, byMonthDay, bySetPos, until, count}',
  recurrence_end  DATETIME(3)  NULL     COMMENT 'effective end date for recurrence expansion queries',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_events_public_id (public_id),
  KEY idx_events_calendar_start (calendar_id, start_at),
  KEY idx_events_calendar_end (calendar_id, end_at),
  KEY idx_events_recurrence (calendar_id, recurrence_end),
  CONSTRAINT fk_events_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_events_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_events_assigned_to FOREIGN KEY (assigned_to) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/005_event_comments.sql =====
-- Comments on events (TimeTree's in-event chat feature)
CREATE TABLE IF NOT EXISTS event_comments (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  event_id   INT UNSIGNED NOT NULL,
  user_id    INT UNSIGNED NOT NULL,
  body       TEXT         NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_comments_public_id (public_id),
  KEY idx_comments_event (event_id, created_at),
  CONSTRAINT fk_comments_event FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_comments_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/006_memos.sql =====
-- Keep / Memo feature (shared to-do lists within a calendar)
CREATE TABLE IF NOT EXISTS memos (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id   BINARY(16)   NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  title       VARCHAR(500) NOT NULL,
  done        TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order  INT          NOT NULL DEFAULT 0,
  created_by  INT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_memos_public_id (public_id),
  KEY idx_memos_calendar (calendar_id, sort_order),
  CONSTRAINT fk_memos_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_memos_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/007_calendar_invites.sql =====
-- Invitation links for calendars (TimeTree's invite-by-link feature)
CREATE TABLE IF NOT EXISTS calendar_invites (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id INT UNSIGNED NOT NULL,
  token       VARCHAR(64)  NOT NULL COMMENT 'unique invite token',
  role        ENUM('admin', 'member', 'viewer') NOT NULL DEFAULT 'member',
  max_uses    INT UNSIGNED NULL     COMMENT 'null = unlimited',
  use_count   INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at  DATETIME(3)  NULL,
  created_by  INT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_invites_token (token),
  KEY idx_invites_calendar (calendar_id),
  CONSTRAINT fk_invites_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_invites_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/008_event_participants.sql =====
CREATE TABLE IF NOT EXISTS event_participants (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  event_id   INT UNSIGNED NOT NULL,
  user_id    INT UNSIGNED NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_ep (event_id, user_id),
  KEY idx_ep_user (user_id),
  CONSTRAINT fk_ep_event FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_ep_user  FOREIGN KEY (user_id)  REFERENCES users  (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/009_event_checklist_items.sql =====
CREATE TABLE IF NOT EXISTS event_checklist_items (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id  BINARY(16)   NOT NULL,
  event_id   INT UNSIGNED NOT NULL,
  title      VARCHAR(500) NOT NULL,
  done       TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order INT          NOT NULL DEFAULT 0,
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_ci_pub (public_id),
  KEY idx_ci_event (event_id, sort_order),
  CONSTRAINT fk_ci_event FOREIGN KEY (event_id) REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_ci_user  FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ===== tables/010_event_attachments.sql =====
CREATE TABLE IF NOT EXISTS event_attachments (
  id           INT UNSIGNED  NOT NULL AUTO_INCREMENT,
  public_id    BINARY(16)    NOT NULL,
  event_id     INT UNSIGNED  NOT NULL,
  uploaded_by  INT UNSIGNED  NOT NULL,
  filename     VARCHAR(500)  NOT NULL,
  content_type VARCHAR(255)  NOT NULL DEFAULT 'application/octet-stream',
  byte_size    BIGINT        NOT NULL DEFAULT 0,
  storage_key  VARCHAR(1000) NOT NULL COMMENT 'MinIO object key',
  enabled      TINYINT(1)    NOT NULL DEFAULT 1 COMMENT 'soft delete flag',
  created_at   DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_att_pub (public_id),
  KEY idx_att_event (event_id, enabled),
  CONSTRAINT fk_att_event FOREIGN KEY (event_id)    REFERENCES events (id) ON DELETE CASCADE,
  CONSTRAINT fk_att_user  FOREIGN KEY (uploaded_by)  REFERENCES users  (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

