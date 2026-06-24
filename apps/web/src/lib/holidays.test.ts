import { describe, expect, it } from 'vitest';
import { getHoliday, HOLIDAY_COUNTRIES } from './holidays';

describe('HOLIDAY_COUNTRIES', () => {
  it('includes Japan as a supported country', () => {
    const jp = HOLIDAY_COUNTRIES.find((c) => c.code === 'JP');
    expect(jp).toBeDefined();
    expect(jp?.nameEn).toBe('Japan');
  });
});

describe('getHoliday', () => {
  it('returns the New Year holiday for Japan on Jan 1', () => {
    const holiday = getHoliday('JP', '2026-01-01');
    expect(holiday).not.toBeNull();
    expect(holiday?.date).toBe('2026-01-01');
    expect(holiday?.name).toBeTruthy();
    expect(holiday?.type).toBe('public');
  });

  it('returns null for an ordinary weekday', () => {
    // 2026-06-17 is a Wednesday with no Japanese public holiday.
    expect(getHoliday('JP', '2026-06-17')).toBeNull();
  });

  it('returns null when no country is selected', () => {
    expect(getHoliday(null, '2026-01-01')).toBeNull();
  });

  it('returns null for a malformed date', () => {
    expect(getHoliday('JP', 'not-a-date')).toBeNull();
  });

  it('serves repeated lookups from cache (stable result)', () => {
    const a = getHoliday('JP', '2026-01-01');
    const b = getHoliday('JP', '2026-01-01');
    expect(a).toEqual(b);
  });

  it('distinguishes holidays between countries', () => {
    // US Independence Day is a holiday; Japan has nothing on Jul 4.
    expect(getHoliday('US', '2026-07-04')).not.toBeNull();
    expect(getHoliday('JP', '2026-07-04')).toBeNull();
  });
});
