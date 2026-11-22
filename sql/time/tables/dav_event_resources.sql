-- ====================================
-- dav_event_resources
-- The resource name and UID a CalDAV client gave an event it created. An
-- event with no row here is served under a name derived from its public ID.
-- The calendar is read through calendar_events, not stored, so an event
-- that moves calendars cannot leave a stale copy behind.
-- ====================================
CREATE TABLE dav_event_resources (
  event_id INT UNSIGNED NOT NULL PRIMARY KEY COMMENT 'Internal FK to calendar_events.id',
  href_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'Resource name chosen by the client; compared case-sensitively',
  uid VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL COMMENT 'iCalendar UID chosen by the client; compared case-sensitively',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  KEY idx_dav_event_resources_href_name (href_name),
  KEY idx_dav_event_resources_uid (uid),

  CONSTRAINT fk_dav_event_resources_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CalDAV resource name and UID per event';
