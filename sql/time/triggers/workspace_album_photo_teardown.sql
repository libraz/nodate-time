-- Flow deletes a workspace without naming album_photos, whose RESTRICT on
-- storage_objects would refuse the cascade. Queue every photo key, its
-- .thumb, and every object key the photos point at, then delete the photos.
-- No held filter: the objects holding these keys cascade right after, and a
-- photo confirmed after Flow listed the workspace keys is queued only here.
-- No reference is released, because the objects go with the workspace.
DELIMITER $$
CREATE TRIGGER workspace_album_photo_teardown
BEFORE DELETE ON workspaces FOR EACH ROW
BEGIN
  DECLARE locked_rows INT;

  SELECT COUNT(*) INTO locked_rows FROM album_photos
  WHERE workspace_id = OLD.id FOR UPDATE;

  -- A key (or key plus '.thumb') over 255 characters cannot exist in
  -- storage_objects or the queue.
  INSERT INTO storage_blob_removals (storage_key)
  SELECT k.storage_key FROM (
    SELECT CONVERT(ap.storage_key USING latin1) AS storage_key
    FROM album_photos ap
    WHERE ap.workspace_id = OLD.id AND ap.storage_key <> '' AND CHAR_LENGTH(ap.storage_key) <= 255
    UNION
    SELECT CONVERT(CONCAT(ap.storage_key, '.thumb') USING latin1)
    FROM album_photos ap
    WHERE ap.workspace_id = OLD.id AND ap.storage_key <> '' AND CHAR_LENGTH(ap.storage_key) <= 249
    UNION
    SELECT so.storage_key
    FROM album_photos ap INNER JOIN storage_objects so ON so.id = ap.storage_object_id
    WHERE ap.workspace_id = OLD.id
    UNION
    SELECT so.storage_key
    FROM album_photos ap INNER JOIN storage_objects so ON so.id = ap.thumbnail_object_id
    WHERE ap.workspace_id = OLD.id
  ) k
  ON DUPLICATE KEY UPDATE storage_key = VALUES(storage_key);

  DELETE FROM album_photos WHERE workspace_id = OLD.id;
END$$
DELIMITER ;
