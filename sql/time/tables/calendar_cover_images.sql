-- The active uploaded cover for a shared calendar.
CREATE TABLE calendar_cover_images (
  calendar_id INT UNSIGNED NOT NULL PRIMARY KEY,
  storage_object_id INT UNSIGNED NOT NULL,
  CONSTRAINT fk_calendar_cover_images_calendar
    FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_cover_images_object
    FOREIGN KEY (storage_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Uploaded cover images for shared calendars';
