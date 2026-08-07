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

/** The wall-clock shape the editor's date and time fields hold. */
const WALL_CLOCK = "yyyy-MM-dd'T'HH:mm";

/**
 * Moves an event's start onto `newStartDate`, keeping the time of day and the
 * span to the end.
 *
 * The span is a calendar length, not a number of milliseconds. A zone that
 * gains or loses an hour between the two ends makes those two quantities
 * disagree, so re-applying an absolute offset moves an end the user never
 * touched: an event running to 10:00 comes back reading 09:00 or 11:00 purely
 * because its start was dragged past a transition.
 */
export function shiftStartKeepingDuration(
  form: EventTimeForm,
  newStartDate: DateTime,
  zone: string,
): EventTimes {
  const time = form.startAt.split('T')[1] ?? '00:00';
  const newStart = DateTime.fromISO(`${newStartDate.toFormat('yyyy-MM-dd')}T${time}`, { zone });
  const oldStart = DateTime.fromISO(form.startAt, { zone });
  const oldEnd = DateTime.fromISO(form.endAt, { zone });
  // Measured on a fixed-offset clock so the transition between the old start
  // and the old end is not charged to the event, then re-applied as calendar
  // units so the new position's own transitions are.
  const span = oldEnd
    .setZone('UTC', { keepLocalTime: true })
    .diff(oldStart.setZone('UTC', { keepLocalTime: true }), ['days', 'hours', 'minutes']);
  const newEnd = span.toMillis() > 0 ? newStart.plus(span) : newStart;
  return {
    startAt: newStart.toFormat(WALL_CLOCK),
    endAt: newEnd.toFormat(WALL_CLOCK),
  };
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
