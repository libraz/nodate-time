import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { publicEventOccursOnDay, shareFailureOf } from './public-calendar';

describe('publicEventOccursOnDay', () => {
  it('buckets events by the event timezone rather than the viewer timezone', () => {
    const event = {
      startAt: '2026-04-01T23:00:00-05:00',
      endAt: '2026-04-01T23:30:00-05:00',
      timezone: 'America/Chicago',
    };

    expect(
      publicEventOccursOnDay(
        event,
        DateTime.fromISO('2026-04-01T00:00:00+09:00', { setZone: true }),
      ),
    ).toBe(true);
    expect(
      publicEventOccursOnDay(
        event,
        DateTime.fromISO('2026-04-02T00:00:00+09:00', { setZone: true }),
      ),
    ).toBe(false);
  });
});

describe('shareFailureOf', () => {
  it('separates a link that is gone from a server that is busy', () => {
    // The owner's reaction to each is different, and only one of them is
    // right: told the link expired, they issue a new one and every embed
    // already published against the old one stops working.
    expect(shareFailureOf({ status: 404 })).toBe('gone');
    expect(shareFailureOf({ status: 410 })).toBe('gone');
    expect(shareFailureOf({ status: 429 })).toBe('busy');
    expect(shareFailureOf({ status: 500 })).toBe('busy');
    expect(shareFailureOf({ status: 502 })).toBe('busy');
  });

  it('treats a failure with nothing to go on as busy', () => {
    // A dropped connection says nothing about the link, and guessing that it
    // died is the guess with a destructive fix attached.
    expect(shareFailureOf(new Error('network'))).toBe('busy');
    expect(shareFailureOf(null)).toBe('busy');
    expect(shareFailureOf(undefined)).toBe('busy');
  });
});
