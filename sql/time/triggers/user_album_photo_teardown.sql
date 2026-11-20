-- Either application can hard-delete a user. Their uploads and every photo
-- and cover on the calendars they own would cascade (owned calendars fire no
-- trigger) past the RESTRICT on storage_objects, so release and remove them
-- here. Objects are workspace-scoped and survive: only photo keys no
-- confirmed or still-referenced object holds are queued, and the unreferenced
-- sweep reclaims the rest. Lock order is owned calendars, photos, objects,
-- queue, covers, matching the calendar-first request paths. The photo set is
-- two indexed lookups joined by UNION; an OR plans as a full scan.
-- STRAIGHT_JOIN keeps each read driven from the user's rows, so no unrelated
-- row is locked.
DELIMITER $$
CREATE TRIGGER user_album_photo_teardown
BEFORE DELETE ON users FOR EACH ROW
BEGIN
  DECLARE locked_rows INT;

  SELECT COUNT(*) INTO locked_rows FROM calendars
  WHERE owner_user_id = OLD.id FOR UPDATE;

  SELECT COUNT(*) INTO locked_rows FROM album_photos
  WHERE uploaded_by_user_id = OLD.id FOR UPDATE;

  SELECT COUNT(*) INTO locked_rows
  FROM calendars c STRAIGHT_JOIN album_photos ap ON ap.calendar_id = c.id
  WHERE c.owner_user_id = OLD.id
  FOR UPDATE;

  -- A multi-table UPDATE changes a row once, so photos sharing an object are
  -- counted first and released together.
  WITH p AS (
    SELECT id FROM album_photos WHERE uploaded_by_user_id = OLD.id
    UNION
    SELECT ap.id
    FROM calendars c STRAIGHT_JOIN album_photos ap ON ap.calendar_id = c.id
    WHERE c.owner_user_id = OLD.id
  )
  UPDATE storage_objects so
  INNER JOIN (
    SELECT x.object_id, COUNT(*) AS n
    FROM (
      SELECT ap.storage_object_id AS object_id
      FROM p STRAIGHT_JOIN album_photos ap ON ap.id = p.id
      UNION ALL
      SELECT ap.thumbnail_object_id
      FROM p STRAIGHT_JOIN album_photos ap ON ap.id = p.id
    ) x
    WHERE x.object_id IS NOT NULL
    GROUP BY x.object_id
  ) r ON r.object_id = so.id
  SET so.ref_count = GREATEST(so.ref_count, r.n) - r.n;

  -- The photo and .thumb keys are tested independently. A key over 255
  -- characters, '.thumb' included, cannot exist in storage_objects or the queue.
  INSERT INTO storage_blob_removals (storage_key)
  WITH p AS (
    SELECT id FROM album_photos WHERE uploaded_by_user_id = OLD.id
    UNION
    SELECT ap.id
    FROM calendars c STRAIGHT_JOIN album_photos ap ON ap.calendar_id = c.id
    WHERE c.owner_user_id = OLD.id
  )
  SELECT k.storage_key FROM (
    SELECT CONVERT(ap.storage_key USING latin1) AS storage_key
    FROM p STRAIGHT_JOIN album_photos ap ON ap.id = p.id
    WHERE ap.storage_key <> '' AND CHAR_LENGTH(ap.storage_key) <= 255
    UNION
    SELECT CONVERT(CONCAT(ap.storage_key, '.thumb') USING latin1)
    FROM p STRAIGHT_JOIN album_photos ap ON ap.id = p.id
    WHERE ap.storage_key <> '' AND CHAR_LENGTH(ap.storage_key) <= 249
  ) k
  WHERE NOT EXISTS (
    SELECT 1 FROM storage_objects so
    WHERE so.storage_key = k.storage_key
      AND BINARY so.storage_key = BINARY k.storage_key
      AND (so.uploaded_at IS NOT NULL OR so.ref_count > 0)
  )
  ON DUPLICATE KEY UPDATE storage_key = VALUES(storage_key);

  WITH p AS (
    SELECT id FROM album_photos WHERE uploaded_by_user_id = OLD.id
    UNION
    SELECT ap.id
    FROM calendars c STRAIGHT_JOIN album_photos ap ON ap.calendar_id = c.id
    WHERE c.owner_user_id = OLD.id
  )
  DELETE ap FROM p STRAIGHT_JOIN album_photos ap ON ap.id = p.id;

  SELECT COUNT(*) INTO locked_rows
  FROM calendars c STRAIGHT_JOIN calendar_cover_images ci ON ci.calendar_id = c.id
  WHERE c.owner_user_id = OLD.id
  FOR UPDATE;

  -- Two owned calendars may share one cover object.
  UPDATE storage_objects so
  INNER JOIN (
    SELECT ci.storage_object_id AS object_id, COUNT(*) AS n
    FROM calendars c STRAIGHT_JOIN calendar_cover_images ci ON ci.calendar_id = c.id
    WHERE c.owner_user_id = OLD.id
    GROUP BY ci.storage_object_id
  ) r ON r.object_id = so.id
  SET so.ref_count = GREATEST(so.ref_count, r.n) - r.n;

  DELETE ci FROM calendars c STRAIGHT_JOIN calendar_cover_images ci ON ci.calendar_id = c.id
  WHERE c.owner_user_id = OLD.id;
END$$
DELIMITER ;
