-- selftest: accepted
-- An event moved to another calendar after its log row was written: the row
-- keeps the calendar it was written under, which is no longer the event's,
-- and the check must still pass.
INSERT INTO calendars (public_id, workspace_id, name)
VALUES (UUID_TO_BIN(UUID(), 0), @ws, 'conformance moved to');
SET @nf_selftest_cal2 := LAST_INSERT_ID();
INSERT INTO events (public_id, workspace_id, type, payload_json, occurred_at, subject_public_id, calendar_id)
SELECT UUID_TO_BIN(UUID(), 0), @ws, 'calendar.event.updated',
       JSON_OBJECT('eventId', BIN_TO_UUID(ce.public_id, 0), 'title', ce.title), NOW(3), ce.public_id, @cal
FROM calendar_events ce WHERE ce.id = @plain_event;
UPDATE calendar_events SET calendar_id = @nf_selftest_cal2 WHERE id = @plain_event;
