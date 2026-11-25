-- Event labels: the per-calendar label table and the event link table.
-- Idempotent; apply after caldav.sql and before triggers.sql.
-- A table is skipped when it already exists, so compare a hand-made one
-- with its file under sql/time/tables/ first.

SET NAMES utf8mb4;

CREATE TABLE IF NOT EXISTS calendar_labels (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id; a label belongs to one calendar',

  name VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs NOT NULL COMMENT 'Display name, trimmed, unique within the calendar. Accent- and case-sensitive so kana that differ only by voicing or script stay distinct names.',
  color CHAR(7) CHARACTER SET ascii NOT NULL COMMENT 'Upper-case #RRGGBB fill an event carrying this label is drawn in',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order (not exposed yet; ties break on id)',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_labels_public_id (public_id),
  UNIQUE KEY uniq_calendar_labels_calendar_name (calendar_id, name),

  CONSTRAINT fk_calendar_labels_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_labels_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-calendar event labels';

CREATE TABLE IF NOT EXISTS calendar_event_labels (
  event_id INT UNSIGNED NOT NULL PRIMARY KEY COMMENT 'Internal FK to calendar_events.id; one label per event row',
  label_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_labels.id',

  KEY idx_calendar_event_labels_label (label_id),

  CONSTRAINT fk_calendar_event_labels_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_labels_label FOREIGN KEY (label_id) REFERENCES calendar_labels(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Event-to-label links';
