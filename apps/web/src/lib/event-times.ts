import { DateTime } from 'luxon';

export interface EventTimeForm {
  allDay: boolean;
  /** Wall-clock `yyyy-MM-ddTHH:mm` as typed in the editor. */
  startAt: string;
  /** Wall-clock `yyyy-MM-ddTHH:mm`; for an all-day event this is the last day, inclusive. */
  endAt: string;
}

export interface EventTimes {
  startAt: string;
  endAt: string;
}

/**
 * Turns what the editor holds into the instants the API stores.
 *
 * The two zones are not interchangeable. A timed event is an instant, and the
 * editor shows it in the viewer's zone, so that is where its fields are read
 * back from. An all-day event is a pair of dates anchored at midnight in the
 * zone the event belongs to; reading those in the viewer's zone would move the
 * event by the offset between the two every time someone elsewhere opened it
 * and pressed save without changing anything.
 */
export function eventTimesForSave(
  form: EventTimeForm,
  viewerZone: string,
  anchorZone: string,
): EventTimes {
  if (form.allDay) {
    const startDay = DateTime.fromISO(form.startAt, { zone: anchorZone }).startOf('day');
    const typedEnd = DateTime.fromISO(form.endAt, { zone: anchorZone }).startOf('day');
    // The stored end is exclusive, and an end before the start is a typo
    // rather than a request for a negative span.
    const endDay = typedEnd >= startDay ? typedEnd : startDay;
    return {
      startAt: startDay.toISO() ?? form.startAt,
      endAt: endDay.plus({ days: 1 }).toISO() ?? form.endAt,
    };
  }

  const start = DateTime.fromISO(form.startAt, { zone: viewerZone });
  const end = DateTime.fromISO(form.endAt, { zone: viewerZone });
  const effectiveEnd = end > start ? end : start.plus({ hours: 1 });
  return {
    startAt: start.toISO() ?? form.startAt,
    endAt: effectiveEnd.toISO() ?? form.endAt,
  };
}
