import { describe, expect, it } from 'vitest';
import { formatSchedulePollSlotTime } from './schedule-poll-ui';

describe('schedule poll candidate time formatting', () => {
  it('shows the end date for a candidate that crosses midnight', () => {
    const label = formatSchedulePollSlotTime(
      '2026-04-01T22:00:00+09:00',
      '2026-04-02T00:30:00+09:00',
      'Asia/Tokyo',
      'en',
    );

    expect(label).toContain('Apr 1');
    expect(label).toContain('Apr 2');
  });

  it('distinguishes repeated wall-clock times across a daylight-saving change', () => {
    const label = formatSchedulePollSlotTime(
      '2026-11-01T01:30:00-04:00',
      '2026-11-01T01:30:00-05:00',
      'America/New_York',
      'en',
    );

    expect(label).toContain('-04:00');
    expect(label).toContain('-05:00');
  });
});
