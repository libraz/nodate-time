-- 7. A task projection at a Tokyo midnight: the nt shape contradicts task_id.
-- expect: unclassified 0201

SET time_zone = '+00:00';
SET FOREIGN_KEY_CHECKS = 0;
SET @nf_item_projection_engine = 1;
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, task_id, task_role)
VALUES
  (201, UNHEX('01900000000070008000000000000201'), 1, 1, 'g7 task at a zone midnight', 1,
   '2026-06-30 15:00:00', '2026-07-01 15:00:00', 'Asia/Tokyo', 1, 1, 2, 'due');
SET @nf_item_projection_engine = NULL;
SET FOREIGN_KEY_CHECKS = 1;
