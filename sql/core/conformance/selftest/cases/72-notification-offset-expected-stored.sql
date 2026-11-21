-- The row helper must fail when an out-of-range offset expected to be stored is refused.
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', 10081",
  'chk_calendar_events_notification_offset_range', TRUE, '72 offset 10081 expected to be stored');
