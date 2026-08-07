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

const MINUTES_PER_DAY = 24 * 60;

/**
 * Where `dt` sits inside the day `dayStart` belongs to, as minutes on the wall
 * clock, clamped to that day.
 *
 * A timeline draws 24 equal hours and labels them with wall-clock hours, so a
 * position inside it is a wall-clock quantity. Elapsed time is a different
 * measure: the day a zone springs forward is 23 hours long, so counting from
 * midnight in elapsed milliseconds draws everything after the transition an
 * hour above the hour line it is labelled with.
 */
export function minutesIntoDay(dt: DateTime, dayStart: DateTime): number {
  const start = dayStart.startOf('day');
  if (dt <= start) return 0;
  if (dt >= start.plus({ days: 1 })) return MINUTES_PER_DAY;
  // Read in the day's own zone: the hour is what the position is made of.
  const local = dt.setZone(start.zone);
  return local.hour * 60 + local.minute;
}

/**
 * The instant `minutes` wall-clock minutes into the day `dayStart` belongs to.
 *
 * The inverse of {@link minutesIntoDay}, and what a click at a position on the
 * timeline means. Adding the minutes to midnight as elapsed time instead lands
 * an hour away from the slot that was clicked on a transition day, and that is
 * the time the event is stored with.
 */
export function atMinutesIntoDay(dayStart: DateTime, minutes: number): DateTime {
  const start = dayStart.startOf('day');
  const clamped = Math.min(Math.max(minutes, 0), MINUTES_PER_DAY);
  if (clamped === MINUTES_PER_DAY) return start.plus({ days: 1 });
  return start.set({
    hour: Math.floor(clamped / 60),
    minute: clamped % 60,
    second: 0,
    millisecond: 0,
  });
}

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
