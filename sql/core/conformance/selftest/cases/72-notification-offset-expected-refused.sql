-- The row helper must fail when an in-range offset expected to be refused is stored.
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', 10080",
  'chk_calendar_events_notification_offset_range', FALSE, '72 offset 10080 expected to be refused');
