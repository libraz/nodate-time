-- Occurrence keys of an nt Tokyo series too far from midnight to name a
-- day: an exception three hours off, one that does not parse, an override
-- key three hours off and a reminder claim two hours off.
-- expect: exception_key 0210
-- expect: override_key 0211
-- expect: reminder_key 0210

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule, recurrence_exceptions,
   recurrence_parent_id, recurrence_original_start)
VALUES
  (210, UNHEX('01900000000070008000000000000210'), 1, 1, 'nt parent with distant keys', 1,
   '2026-12-10 15:00:00', '2026-12-11 15:00:00', 'Asia/Tokyo', 1, 1, '{"freq":"daily"}',
   '["2026-12-15T12:00:00Z", "2026-02-30T00:00:00Z", "2026-12-16"]', NULL, NULL),
  (211, UNHEX('01900000000070008000000000000211'), 1, 1, 'override three hours off', 0,
   '2026-12-12 05:00:00', '2026-12-12 06:00:00', 'Asia/Tokyo', 1, 1, NULL, NULL, 210, '2026-12-11 18:00:00');
INSERT INTO calendar_event_reminders (id, workspace_id, event_id, occurrence_start) VALUES
  (7, 1, 210, '2026-12-10 13:00:00');
SET FOREIGN_KEY_CHECKS = 1;
