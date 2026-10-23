import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { formatDateTime } from './format-datetime';

describe('formatDateTime', () => {
  it('reads the instant in the given zone, whatever the device zone is', () => {
    const iso = '2026-08-01T15:30:00Z';
    expect(formatDateTime(iso, 'Asia/Tokyo', 'en')).toContain('12:30');
    expect(formatDateTime(iso, 'America/New_York', 'en')).toContain('11:30');
  });

  it('can format the date alone', () => {
    const out = formatDateTime('2026-08-01T20:00:00Z', 'Asia/Tokyo', 'en', DateTime.DATE_SHORT);
    expect(out).toBe('8/2/2026');
  });
});
