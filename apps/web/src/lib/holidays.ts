import type Holidays from 'date-holidays';

export interface HolidayInfo {
  date: string;
  name: string;
  type: string;
}

const cache = new Map<string, Map<string, HolidayInfo>>();
const pending = new Map<string, Promise<void>>();
let holidaysConstructor: typeof Holidays | null = null;
let holidaysConstructorPromise: Promise<typeof Holidays> | null = null;

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
      const hd = new HolidaysClass(country);
      const list = hd.getHolidays(year) as Array<{ date: string; name: string; type: string }>;
      for (const h of list) {
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
