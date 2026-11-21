-- 20. A zone the time zone tables do not know; no shape can be judged.
-- expect: unclassified 0209
-- expect: unknown_zone 0209

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id)
VALUES
  (209, UNHEX('01900000000070008000000000000209'), 1, 1, 'g20 unknown zone', 1,
   '2026-05-01 00:00:00', '2026-05-02 00:00:00', 'Mars/Olympus', 1, 1);
SET FOREIGN_KEY_CHECKS = 1;
