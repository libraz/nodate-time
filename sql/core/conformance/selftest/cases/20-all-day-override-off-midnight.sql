-- An override of an all-day series keyed to a time of day.
INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'all-day series', TRUE, '2030-03-01 00:00:00',
   '2030-03-02 00:00:00', 'UTC', @usr, @usr, '{"freq":"daily"}');
SET @nf_selftest_parent := LAST_INSERT_ID();
INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_parent_id, recurrence_original_start)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'override', TRUE, '2030-03-05 00:00:00',
   '2030-03-06 00:00:00', 'UTC', @usr, @usr, @nf_selftest_parent, '2030-03-04 15:00:00');
