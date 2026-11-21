-- The all-day row shape and the notification range, held by
-- chk_calendar_events_all_day_canonical and
-- chk_calendar_events_notification_offset_range (PROTOCOL.md §All-day
-- events, §Notification offset).
--
-- Each refused row differs from an accepted one in a single respect, and
-- nf_conformance_expect_event_row insists the refusal names the CHECK
-- under test; a row refused by some other constraint does not count.

SET @nf_ad := 'all_day, kind, start_at, end_at';

-- The constraints exist at all.
CALL nf_conformance_assert(
  (SELECT COUNT(*) = 2 FROM information_schema.check_constraints
   WHERE constraint_schema = DATABASE()
     AND constraint_name IN ('chk_calendar_events_all_day_canonical',
                             'chk_calendar_events_notification_offset_range')),
  '72 both chk_calendar_events_all_day_canonical and chk_calendar_events_notification_offset_range must exist');

-- All-day: UTC midnight on both ends, half-open, milestone a point.
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-03-10 00:00:00', '2030-03-11 00:00:00'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 one-day all-day row');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-02-28 00:00:00', '2030-03-03 00:00:00'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 multi-day all-day row');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'milestone', '2030-03-10 00:00:00', '2030-03-10 00:00:00'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 all-day milestone as a point');
CALL nf_conformance_expect_event_row('all_day, kind',
  "TRUE, 'event'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 undated all-day row');
CALL nf_conformance_expect_event_row(@nf_ad,
  "FALSE, 'event', '2030-03-10 09:30:00', '2030-03-10 10:00:00'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 timed row is not constrained');

CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-03-10 15:00:00', '2030-03-11 00:00:00'",
  'chk_calendar_events_all_day_canonical', FALSE, '72 all-day start not at UTC midnight');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-03-10 00:00:00', '2030-03-11 00:00:00.001'",
  'chk_calendar_events_all_day_canonical', FALSE, '72 all-day end a millisecond past midnight');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-03-10 00:00:00', '2030-03-10 00:00:00'",
  'chk_calendar_events_all_day_canonical', FALSE, '72 non-milestone all-day row with end = start');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'milestone', '2030-03-10 00:00:00', '2030-03-11 00:00:00'",
  'chk_calendar_events_all_day_canonical', FALSE, '72 all-day milestone spanning a day');
CALL nf_conformance_expect_event_row(@nf_ad,
  "TRUE, 'event', '2030-03-10 00:00:00', '2030-03-12 00:00:00'",
  'chk_calendar_events_all_day_canonical', TRUE, '72 two-day all-day row');

-- Notification offset: NULL or 0..10080.
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', NULL",
  'chk_calendar_events_notification_offset_range', TRUE, '72 no reminder');
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', 0",
  'chk_calendar_events_notification_offset_range', TRUE, '72 offset 0');
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', 10080",
  'chk_calendar_events_notification_offset_range', TRUE, '72 offset 10080');
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', 10081",
  'chk_calendar_events_notification_offset_range', FALSE, '72 offset 10081');
CALL nf_conformance_expect_event_row('start_at, end_at, notification_offset',
  "'2030-03-10 09:00:00', '2030-03-10 10:00:00', -1",
  'chk_calendar_events_notification_offset_range', FALSE, '72 offset -1');

SET @nf_ad = NULL;
