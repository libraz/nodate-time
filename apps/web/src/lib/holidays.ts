import Holidays from 'date-holidays';

export interface HolidayInfo {
  date: string;
  name: string;
  type: string;
}

const cache = new Map<string, Map<string, HolidayInfo>>();

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

function buildHolidayMap(country: string, year: number): Map<string, HolidayInfo> {
  const key = `${country}-${year}`;
  const cached = cache.get(key);
  if (cached) return cached;

  const map = new Map<string, HolidayInfo>();
  try {
    const hd = new Holidays(country);
    const list = hd.getHolidays(year) as Array<{ date: string; name: string; type: string }>;
    for (const h of list) {
      const date = h.date.slice(0, 10);
      if (!map.has(date)) {
        map.set(date, { date, name: h.name, type: h.type });
      }
    }
  } catch {
    // unsupported country: leave map empty
  }
  cache.set(key, map);
  return map;
}

export function getHoliday(country: string | null, isoDate: string): HolidayInfo | null {
  if (!country) return null;
  const year = Number(isoDate.slice(0, 4));
  if (!Number.isFinite(year)) return null;
  const map = buildHolidayMap(country, year);
  return map.get(isoDate) ?? null;
}
