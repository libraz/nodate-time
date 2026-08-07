import type Holidays from 'date-holidays';
import type { HolidaysTypes } from 'date-holidays';

export interface HolidayInfo {
  date: string;
  name: string;
  type: string;
}

/** A country the holiday data covers, named in the language the UI is in. */
export interface HolidayCountry {
  code: string;
  name: string;
}

const cache = new Map<string, Map<string, HolidayInfo>>();
const pending = new Map<string, Promise<void>>();
const countryCache = new Map<string, HolidayCountry[]>();
const countryPending = new Map<string, Promise<HolidayCountry[]>>();
let holidaysConstructor: typeof Holidays | null = null;
let holidaysConstructorPromise: Promise<typeof Holidays> | null = null;

/**
 * Only public holidays are days off, and only days off belong on the calendar.
 *
 * The data also carries bank, school, optional and observance days, and the
 * library returns all of them unless asked otherwise. Left at that default,
 * Japan paints Christmas, New Year's Eve and the bank days around it red, and
 * Germany reports thirty holidays where it has nine.
 */
const HOLIDAY_TYPES: HolidaysTypes.HolidayType[] = ['public'];

/**
 * The countries offered until the holiday data has loaded.
 *
 * The picker's real list comes from the data itself; these keep it from being
 * empty on first paint, and name the common cases in both UI languages in case
 * the browser cannot name a region.
 */
export const HOLIDAY_COUNTRIES: { code: string; nameJa: string; nameEn: string }[] = [
  { code: 'JP', nameJa: '日本', nameEn: 'Japan' },
  { code: 'US', nameJa: 'アメリカ', nameEn: 'United States' },
  { code: 'GB', nameJa: 'イギリス', nameEn: 'United Kingdom' },
  { code: 'DE', nameJa: 'ドイツ', nameEn: 'Germany' },
  { code: 'FR', nameJa: 'フランス', nameEn: 'France' },
  { code: 'KR', nameJa: '韓国', nameEn: 'South Korea' },
  { code: 'CN', nameJa: '中国', nameEn: 'China' },
  { code: 'TW', nameJa: '台湾', nameEn: 'Taiwan' },
  { code: 'SG', nameJa: 'シンガポール', nameEn: 'Singapore' },
  { code: 'AU', nameJa: 'オーストラリア', nameEn: 'Australia' },
];

async function loadHolidaysConstructor(): Promise<typeof Holidays> {
  if (holidaysConstructor) return holidaysConstructor;
  holidaysConstructorPromise ??= import('date-holidays').then((module) => module.default);
  holidaysConstructor = await holidaysConstructorPromise;
  return holidaysConstructor;
}

async function buildHolidayMap(country: string, year: number): Promise<void> {
  const key = `${country}-${year}`;
  if (cache.has(key)) return;
  const inFlight = pending.get(key);
  if (inFlight) return inFlight;

  const request = (async () => {
    const map = new Map<string, HolidayInfo>();
    try {
      const HolidaysClass = await loadHolidaysConstructor();
      const hd = new HolidaysClass(country, { types: HOLIDAY_TYPES });
      for (const h of hd.getHolidays(year)) {
        const date = h.date.slice(0, 10);
        if (!map.has(date)) {
          map.set(date, { date, name: h.name, type: h.type });
        }
      }
    } catch {
      // Unsupported country or a failed optional chunk: leave the map empty.
    }
    cache.set(key, map);
    pending.delete(key);
  })();
  pending.set(key, request);
  return request;
}

/** Loads holiday data on demand without adding date-holidays to the initial bundle. */
export async function preloadHolidays(country: string | null, years: number[]): Promise<void> {
  if (!country) return;
  await Promise.all([...new Set(years)].map((year) => buildHolidayMap(country, year)));
}

export function getHoliday(country: string | null, isoDate: string): HolidayInfo | null {
  if (!country) return null;
  const year = Number(isoDate.slice(0, 4));
  if (!Number.isFinite(year)) return null;
  const map = cache.get(`${country}-${year}`);
  if (!map) return null;
  return map.get(isoDate) ?? null;
}

/** The seed list, named for `locale`. */
export function fallbackHolidayCountries(locale: string): HolidayCountry[] {
  const ja = locale.startsWith('ja');
  return HOLIDAY_COUNTRIES.map((c) => ({ code: c.code, name: ja ? c.nameJa : c.nameEn }));
}

/**
 * Names a region code in `locale`, or returns null when the browser has no
 * name for it — the data's own name is a better answer than a bare code.
 */
function regionNamer(locale: string): (code: string) => string | null {
  let display: Intl.DisplayNames;
  try {
    display = new Intl.DisplayNames([locale], { type: 'region' });
  } catch {
    return () => null;
  }
  return (code) => {
    try {
      const name = display.of(code);
      return name && name !== code ? name : null;
    } catch {
      return null;
    }
  };
}

/**
 * Every country the holiday data covers, named and sorted for `locale`.
 *
 * The data knows two hundred of them; offering a handpicked ten tells everyone
 * else their country is unsupported when it is not.
 */
export async function loadHolidayCountries(locale: string): Promise<HolidayCountry[]> {
  const cached = countryCache.get(locale);
  if (cached) return cached;
  const inFlight = countryPending.get(locale);
  if (inFlight) return inFlight;

  const request = (async () => {
    let list = fallbackHolidayCountries(locale);
    try {
      const HolidaysClass = await loadHolidaysConstructor();
      const nameOf = regionNamer(locale);
      const supported = new HolidaysClass().getCountries(locale);
      const entries = Object.entries(supported ?? {});
      if (entries.length > 0) {
        list = entries.map(([code, name]) => ({ code, name: nameOf(code) ?? name }));
      }
    } catch {
      // A failed optional chunk: the seed list keeps the picker usable.
    }
    list.sort(byName(locale));
    countryCache.set(locale, list);
    countryPending.delete(locale);
    return list;
  })();
  countryPending.set(locale, request);
  return request;
}

function byName(locale: string): (a: HolidayCountry, b: HolidayCountry) => number {
  try {
    const collator = new Intl.Collator(locale);
    return (a, b) => collator.compare(a.name, b.name);
  } catch {
    return (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
  }
}

/**
 * The country whose holidays to show before anyone has chosen one.
 *
 * Derived from the browser for the same reason the timezone is: a fixed home
 * country marks the wrong dates as days off for everyone who does not live
 * there. Japan is the fallback when the browser gives nothing to work with.
 */
export function detectHolidayCountry(): string {
  const tags =
    typeof navigator === 'undefined' ? [] : [...(navigator.languages ?? [navigator.language])];
  for (const tag of tags) {
    if (!tag) continue;
    try {
      const region = new Intl.Locale(tag).maximize().region;
      if (region && /^[A-Z]{2}$/.test(region)) return region;
    } catch {
      // A malformed language tag: try the next one.
    }
  }
  return 'JP';
}
