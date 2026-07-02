import { describe, expect, it } from 'vitest';
import { toAllDayInclusiveEndInput, toLocalDatetimeInput } from './all-day';

describe('all-day event form dates', () => {
  it('converts exclusive stored end to inclusive editor end date', () => {
    expect(toAllDayInclusiveEndInput('2026-07-03T00:00:00+09:00', 'Asia/Tokyo')).toBe(
      '2026-07-02T00:00',
    );
  });

  it('keeps regular local datetime rendering unchanged', () => {
    expect(toLocalDatetimeInput('2026-07-02T10:30:00+09:00', 'Asia/Tokyo')).toBe(
      '2026-07-02T10:30',
    );
  });
});
