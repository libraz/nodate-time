-- Only verified objects in the calendar's own workspace can be displayed.
CREATE VIEW v_calendar_cover_images AS
SELECT c.workspace_id, c.id AS calendar_id, so.storage_key
FROM calendar_cover_images ci
JOIN calendars c ON c.id = ci.calendar_id AND c.enabled = TRUE
JOIN storage_objects so ON so.id = ci.storage_object_id AND so.workspace_id = c.workspace_id
WHERE so.enabled = TRUE AND so.uploaded_at IS NOT NULL;
