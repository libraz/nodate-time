-- A cover upload may commit after Flow lists workspace storage keys. Queue
-- cover keys again under the workspace delete lock before their rows cascade.
-- The unique queue key makes this safe when the caller already queued one.
DELIMITER $$
CREATE TRIGGER workspace_calendar_cover_teardown
BEFORE DELETE ON workspaces FOR EACH ROW
BEGIN
  INSERT INTO storage_blob_removals (storage_key)
  SELECT so.storage_key
  FROM storage_objects so
  WHERE so.workspace_id = OLD.id AND so.storage_key LIKE 'calendar-covers/%'
  ON DUPLICATE KEY UPDATE storage_key = VALUES(storage_key);

  DELETE ci FROM calendar_cover_images ci
  INNER JOIN calendars c ON c.id = ci.calendar_id
  WHERE c.workspace_id = OLD.id;
END$$
DELIMITER ;
