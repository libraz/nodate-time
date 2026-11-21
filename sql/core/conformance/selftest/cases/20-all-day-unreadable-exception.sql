-- An all-day series holding an element that is not a date at all.
INSERT INTO calendar_events
  (public_id, workspace_id, calendar_id, title, all_day, start_at, end_at, timezone,
   owner_user_id, created_by_user_id, recurrence_rule, recurrence_exceptions)
VALUES
  (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'all-day series', TRUE, '2030-03-01 00:00:00',
   '2030-03-02 00:00:00', 'UTC', @usr, @usr, '{"freq":"daily"}', '["2030-03-04", "garbage"]');
