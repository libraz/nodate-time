-- The row helper must fail when a canonical all-day row expected to be refused is stored.
CALL nf_conformance_expect_event_row('all_day, kind, start_at, end_at',
  "TRUE, 'event', '2030-03-10 00:00:00', '2030-03-11 00:00:00'",
  'chk_calendar_events_all_day_canonical', FALSE, '72 canonical row expected to be refused');
