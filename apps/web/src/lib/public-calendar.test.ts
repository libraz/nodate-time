import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { publicEventOccursOnDay } from './public-calendar';

describe('publicEventOccursOnDay', () => {
  it('buckets events by the event timezone rather than the viewer timezone', () => {
    const event = {
      startAt: '2026-04-01T23:00:00-05:00',
      endAt: '2026-04-01T23:30:00-05:00',
      timezone: 'America/Chicago',
    };

    expect(publicEventOccursOnDay(event, DateTime.fromISO('2026-04-01T00:00:00+09:00'))).toBe(true);
    expect(publicEventOccursOnDay(event, DateTime.fromISO('2026-04-02T00:00:00+09:00'))).toBe(
      false,
    );
  });
});
