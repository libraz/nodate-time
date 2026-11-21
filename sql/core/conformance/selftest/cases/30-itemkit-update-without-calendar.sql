-- The row an itemkit projection writes for an update: a calendar.event.*
-- kind about an existing event, with no calendar_id.
INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at, subject_public_id)
SELECT UUID_TO_BIN(UUID(), 0), @ws, 'calendar.event.updated',
       JSON_OBJECT('eventId', BIN_TO_UUID(ce.public_id, 0), 'title', ce.title), NOW(3), ce.public_id
FROM calendar_events ce WHERE ce.id = @plain_event;
