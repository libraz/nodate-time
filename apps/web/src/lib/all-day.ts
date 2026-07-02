import { DateTime } from 'luxon';

/** Renders a stored instant as a `yyyy-MM-ddTHH:mm` string in the given zone. */
export function toLocalDatetimeInput(iso: string, zone: string): string {
  return DateTime.fromISO(iso, { zone }).toFormat("yyyy-MM-dd'T'HH:mm");
}

/**
 * Converts an exclusive all-day end instant into the inclusive date shown in
 * the editor. A single-day all-day event stored as Jul 2 00:00 -> Jul 3 00:00
 * must display End = Jul 2, not Jul 3.
 */
export function toAllDayInclusiveEndInput(iso: string, zone: string): string {
  return DateTime.fromISO(iso, { zone }).minus({ days: 1 }).toFormat("yyyy-MM-dd'T'HH:mm");
}
