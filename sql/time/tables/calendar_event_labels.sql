-- The label an event row is filed under; no row means none.
CREATE TABLE calendar_event_labels (
  event_id INT UNSIGNED NOT NULL PRIMARY KEY COMMENT 'Internal FK to calendar_events.id; one label per event row',
  label_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendar_labels.id',

  KEY idx_calendar_event_labels_label (label_id),

  CONSTRAINT fk_calendar_event_labels_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_labels_label FOREIGN KEY (label_id) REFERENCES calendar_labels(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Event-to-label links';
