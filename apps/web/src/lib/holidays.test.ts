import { beforeAll, describe, expect, it } from 'vitest';
import {
  detectHolidayCountry,
  getHoliday,
  HOLIDAY_COUNTRIES,
  loadHolidayCountries,
  preloadHolidays,
} from './holidays';

describe('HOLIDAY_COUNTRIES', () => {
  it('includes Japan as a supported country', () => {
    const jp = HOLIDAY_COUNTRIES.find((c) => c.code === 'JP');
    expect(jp).toBeDefined();
    expect(jp?.nameEn).toBe('Japan');
  });
});

describe('getHoliday', () => {
  beforeAll(async () => {
    await preloadHolidays('JP', [2026]);
    await preloadHolidays('US', [2026]);
    await preloadHolidays('DE', [2026]);
  });
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

  it('leaves out days Japan works through', () => {
    // Christmas and New Year's Eve are observances, Jan 2-3 and Nov 15 bank
    // or observance days: none of them is a day off.
    expect(getHoliday('JP', '2026-12-25')).toBeNull();
    expect(getHoliday('JP', '2026-12-31')).toBeNull();
    expect(getHoliday('JP', '2026-01-02')).toBeNull();
    expect(getHoliday('JP', '2026-01-03')).toBeNull();
    expect(getHoliday('JP', '2026-11-15')).toBeNull();
  });

  it('counts the public holidays Germany actually has', () => {
    const dates = [
      '2026-01-01',
      '2026-04-03',
      '2026-04-06',
      '2026-05-01',
      '2026-05-14',
      '2026-05-25',
      '2026-10-03',
      '2026-12-25',
      '2026-12-26',
    ];
    for (const date of dates) {
      expect(getHoliday('DE', date), date).not.toBeNull();
    }

    const found = countHolidaysIn('DE', 2026);
    expect(found).toBe(dates.length);
  });

  it('counts the public holidays Japan actually has', () => {
    // 16 statutory holidays, plus one substitute day and one bridge day.
    expect(countHolidaysIn('JP', 2026)).toBe(18);
  });
});

/** Walks a whole year a day at a time, since the cache is not exposed. */
function countHolidaysIn(country: string, year: number): number {
  let found = 0;
  const start = Date.UTC(year, 0, 1);
  const end = Date.UTC(year + 1, 0, 1);
  for (let t = start; t < end; t += 86_400_000) {
    const iso = new Date(t).toISOString().slice(0, 10);
    if (getHoliday(country, iso)) found += 1;
  }
  return found;
}

describe('loadHolidayCountries', () => {
  it('offers every country the data covers, not a handpicked few', async () => {
    const countries = await loadHolidayCountries('en');
    expect(countries.length).toBeGreaterThan(200);
    expect(countries.map((c) => c.code)).toContain('IS');
    expect(countries.map((c) => c.code)).toContain('ZA');
  });

  it('names and sorts them for the language in use', async () => {
    const ja = await loadHolidayCountries('ja');
    expect(ja.find((c) => c.code === 'DE')?.name).toBe('ドイツ');

    const en = await loadHolidayCountries('en');
    expect(en.find((c) => c.code === 'DE')?.name).toBe('Germany');
    const names = en.map((c) => c.name);
    expect([...names].sort(new Intl.Collator('en').compare)).toEqual(names);
  });
});

describe('detectHolidayCountry', () => {
  it('reads the region out of the browser languages', () => {
    // jsdom reports en-US; the region has to come from there, not from a
    // hardcoded home country.
    expect(navigator.language.startsWith('en')).toBe(true);
    expect(detectHolidayCountry()).toBe('US');
  });

  it('falls back to Japan when the browser offers nothing usable', () => {
    const original = Object.getOwnPropertyDescriptor(navigator, 'languages');
    Object.defineProperty(navigator, 'languages', { value: ['!'], configurable: true });
    try {
      expect(detectHolidayCountry()).toBe('JP');
    } finally {
      if (original) Object.defineProperty(navigator, 'languages', original);
    }
  });
});
