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

const DATE_FORMAT = 'yyyy-MM-dd';

/** The dates an all-day event carries on the wire. `endDate` is exclusive, except on a milestone where it equals `startDate`. */
export interface AllDayWire {
  startDate?: string | undefined;
  endDate?: string | undefined;
}

/** The first and last day (both inclusive) an all-day event covers. */
export interface AllDayDates {
  firstDate: string;
  lastDate: string;
}

/** Shifts a `yyyy-MM-dd` date by whole days. Dates carry no zone, so this is calendar arithmetic. */
export function addDays(date: string, days: number): string {
  return DateTime.fromISO(date, { zone: 'UTC' }).plus({ days }).toFormat(DATE_FORMAT);
}

/** Whole days from `from` to `to`, both `yyyy-MM-dd`. */
export function daysBetween(from: string, to: string): number {
  const a = DateTime.fromISO(from, { zone: 'UTC' });
  const b = DateTime.fromISO(to, { zone: 'UTC' });
  return Math.round(b.diff(a, 'days').days);
}

/**
 * The days an all-day event covers, read from its wire dates. They are the
 * same for every viewer: no zone is involved. Null when the event carries no
 * dates.
 */
export function allDayDates(evt: AllDayWire): AllDayDates | null {
  const { startDate, endDate } = evt;
  if (!startDate || !endDate) return null;
  // A milestone's end equals its start; every other end is exclusive.
  const lastDate = endDate > startDate ? addDays(endDate, -1) : startDate;
  return { firstDate: startDate, lastDate };
}

/**
 * The wire dates of an all-day event, falling back to its stored instants
 * (UTC midnights) for a row that did not carry them.
 */
export function allDayWireDates(evt: AllDayWire & { startAt: string; endAt: string }): {
  startDate: string;
  endDate: string;
} {
  return {
    startDate:
      evt.startDate ?? DateTime.fromISO(evt.startAt, { zone: 'UTC' }).toFormat(DATE_FORMAT),
    endDate: evt.endDate ?? DateTime.fromISO(evt.endAt, { zone: 'UTC' }).toFormat(DATE_FORMAT),
  };
}
