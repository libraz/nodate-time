import { describe, expect, it } from 'vitest';
import { getMonthDays } from './date-utils';

describe('month grid day boundaries', () => {
  it.each([
    [2026, 3, 'Asia/Tokyo', 35],
    [2026, 7, 'UTC', 42],
    [2026, 9, 'America/New_York', 35],
    [2026, 1, 'America/New_York', 35],
    [2026, 2, 'America/New_York', 35],
    [2026, 10, 'America/New_York', 35],
  ] as const)(
    'anchors every cell of %i-%i in %s to the start of its day',
    (year, month, zone, cells) => {
      const days = getMonthDays(year, month, zone);
      expect(days).toHaveLength(cells);
      for (const [index, day] of days.entries()) {
        expect(day.toMillis(), day.toISO() ?? '').toBe(day.startOf('day').toMillis());
        expect(day.zoneName).toBe(zone);
        const previous = days[index - 1];
        if (previous) {
          expect(day.toMillis()).toBe(previous.plus({ days: 1 }).toMillis());
        }
      }
    },
  );
});
