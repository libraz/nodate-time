import type { DateTimeFormatOptions } from 'luxon';
import { DateTime } from 'luxon';
import { fromISOInZone } from '@/lib/date-utils';

/**
 * Formats an ISO timestamp for display in the account's timezone and the
 * reader's language, rather than the device's own zone.
 */
export function formatDateTime(
  iso: string,
  zone: string,
  locale: string,
  format: DateTimeFormatOptions = DateTime.DATETIME_SHORT,
): string {
  return fromISOInZone(iso, zone).setLocale(locale).toLocaleString(format);
}
