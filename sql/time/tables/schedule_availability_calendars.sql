-- A calendar's managers can contribute aggregate busy time to other
-- schedule polls in the same workspace. No row means no sharing.
CREATE TABLE schedule_availability_calendars (
  calendar_id INT UNSIGNED NOT NULL PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  CONSTRAINT fk_schedule_availability_calendars_calendar
    FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Source calendar consent to workspace schedule availability checks';
