import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import {
  bucketEventsByWeek,
  eventOccupiesDay,
  eventStartDay,
  isMultiDay,
  layoutDayCell,
  layoutWeek,
  MAX_VISIBLE_TRACKS,
  weekKey,
  weekKeysInRange,
} from './week-layout';

const ZONE = 'Asia/Tokyo';

function makeEvent(
  overrides: Partial<CalendarEvent> & { startAt: string; endAt: string },
): CalendarEvent {
  return {
    id: overrides.id ?? Math.random().toString(36).slice(2),
    calendarId: 'cal',
    title: overrides.title ?? 'Event',
    allDay: overrides.allDay ?? false,
    color: '#47B2F7',
    ownerId: null,
    showAs: 'busy',
    flexibility: 'fixed',
    visibility: 'default',
    location: '',
    memo: '',
    url: '',
    notificationOffset: null,
    participants: [],
    recurrenceRule: null,
    isRecurrence: false,
    recurrenceDate: null,
    createdAt: '',
    updatedAt: '',
    ...overrides,
  };
}

describe('isMultiDay', () => {
  it('is false for a same-day event', () => {
    const evt = makeEvent({
      startAt: '2026-04-20T09:00:00+09:00',
      endAt: '2026-04-20T18:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is true for an event spanning two days', () => {
    const evt = makeEvent({
      startAt: '2026-04-20T09:00:00+09:00',
      endAt: '2026-04-21T10:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(true);
  });

  it('is false for a timed event ending exactly at midnight', () => {
    // 23:00 -> next day 00:00 occupies only the first day.
    const evt = makeEvent({
      startAt: '2026-04-20T23:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is false for a single all-day event (exclusive end)', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is true for a two-day all-day event (exclusive end)', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-22T00:00:00+09:00',
    });
    expect(isMultiDay(evt, ZONE)).toBe(true);
  });
});

// An all-day event is a span of dates, not of instants. Read in the viewer's
// zone instead of its own, a one-day event created in Tokyo becomes a two-day
// bar starting the day before for a viewer in New York.
describe('all-day events read in a viewer zone behind the event zone', () => {
  const VIEWER = 'America/New_York';
  const tokyoBirthday = makeEvent({
    allDay: true,
    timezone: 'Asia/Tokyo',
    startAt: '2026-08-05T00:00:00+09:00',
    endAt: '2026-08-06T00:00:00+09:00',
  });

  it('stays a single day', () => {
    expect(isMultiDay(tokyoBirthday, VIEWER)).toBe(false);
  });

  it('stays on the date it was created for', () => {
    expect(eventStartDay(tokyoBirthday, VIEWER).toFormat('yyyy-MM-dd')).toBe('2026-08-05');
  });

  it('occupies that day and not the one before it', () => {
    const fifth = DateTime.fromISO('2026-08-05T00:00:00', { zone: VIEWER });
    expect(eventOccupiesDay(tokyoBirthday, fifth, VIEWER)).toBe(true);
    expect(eventOccupiesDay(tokyoBirthday, fifth.minus({ days: 1 }), VIEWER)).toBe(false);
  });

  it('still reads a timed event in the viewer zone', () => {
    // 09:00 Tokyo on the 5th is 20:00 New York on the 4th, and a viewer in
    // New York needs to see it on the 4th.
    const meeting = makeEvent({
      timezone: 'Asia/Tokyo',
      startAt: '2026-08-05T09:00:00+09:00',
      endAt: '2026-08-05T10:00:00+09:00',
    });
    expect(eventStartDay(meeting, VIEWER).toFormat('yyyy-MM-dd')).toBe('2026-08-04');
  });
});

describe('layoutWeek', () => {
  const weekStart = DateTime.fromISO('2026-04-19T00:00:00', { zone: ZONE }); // Sunday

  it('positions a single multi-day event with correct columns', () => {
    const evt = makeEvent({
      title: 'Trip',
      startAt: '2026-04-20T00:00:00+09:00', // Monday (col 1)
      endAt: '2026-04-23T00:00:00+09:00', // exclusive end -> last day Wednesday (col 3)
    });
    const result = layoutWeek(weekStart, [evt], ZONE);
    expect(result).toHaveLength(1);
    const positioned = result[0];
    expect(positioned?.startCol).toBe(1);
    expect(positioned?.span).toBe(3);
    expect(positioned?.track).toBe(0);
    expect(positioned?.continuesLeft).toBe(false);
    expect(positioned?.continuesRight).toBe(false);
  });

  it('excludes single-day events (rendered as chips, not bars)', () => {
    const timed = makeEvent({
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });
    const nightToMidnight = makeEvent({
      startAt: '2026-04-20T23:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    const singleAllDay = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-21T00:00:00+09:00',
    });
    expect(layoutWeek(weekStart, [timed, nightToMidnight, singleAllDay], ZONE)).toHaveLength(0);
  });

  it('clamps events that overflow the week and flags continuation', () => {
    const evt = makeEvent({
      startAt: '2026-04-15T00:00:00+09:00', // before this week
      endAt: '2026-04-30T00:00:00+09:00', // after this week
    });
    const result = layoutWeek(weekStart, [evt], ZONE);
    const positioned = result[0];
    expect(positioned?.startCol).toBe(0);
    expect(positioned?.span).toBe(7);
    expect(positioned?.continuesLeft).toBe(true);
    expect(positioned?.continuesRight).toBe(true);
  });

  it('places overlapping events on separate tracks', () => {
    const a = makeEvent({
      id: 'a',
      startAt: '2026-04-20T00:00:00+09:00',
      endAt: '2026-04-22T00:00:00+09:00',
    });
    const b = makeEvent({
      id: 'b',
      startAt: '2026-04-21T00:00:00+09:00',
      endAt: '2026-04-23T00:00:00+09:00',
    });
    const result = layoutWeek(weekStart, [a, b], ZONE);
    const tracks = result.map((p) => p.track).sort();
    expect(tracks).toEqual([0, 1]);
  });

  it('reuses a track when events do not overlap', () => {
    const a = makeEvent({
      id: 'a',
      startAt: '2026-04-19T00:00:00+09:00', // Sunday (col 0)
      endAt: '2026-04-21T00:00:00+09:00', // exclusive end -> through Monday (col 1)
    });
    const b = makeEvent({
      id: 'b',
      startAt: '2026-04-23T00:00:00+09:00', // Thursday (col 4)
      endAt: '2026-04-25T00:00:00+09:00', // exclusive end -> through Friday (col 5)
    });
    const result = layoutWeek(weekStart, [a, b], ZONE);
    expect(result).toHaveLength(2);
    expect(result.every((p) => p.track === 0)).toBe(true);
  });

  it('excludes events that fall entirely outside the week', () => {
    const evt = makeEvent({
      startAt: '2026-05-01T00:00:00+09:00',
      endAt: '2026-05-03T00:00:00+09:00',
    });
    expect(layoutWeek(weekStart, [evt], ZONE)).toHaveLength(0);
  });

  it('exposes a sensible visible-track limit', () => {
    expect(MAX_VISIBLE_TRACKS).toBeGreaterThan(0);
  });

  it('gives the longest bar the lowest track when several start together', () => {
    // Three short bars listed before the long one: on plain first-fit the long
    // bar lands last, on a track no cell draws, and vanishes for the whole week.
    const shorts = ['s1', 's2', 's3'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-23T00:00:00+09:00' }),
    );
    const long = makeEvent({
      id: 'long',
      title: 'Long trip',
      startAt: '2026-04-19T00:00:00+09:00',
      endAt: '2026-04-26T00:00:00+09:00',
    });
    const result = layoutWeek(weekStart, [...shorts, long], ZONE);
    expect(result.find((p) => p.event.id === 'long')?.track).toBe(0);
  });
});

describe('bucketEventsByWeek', () => {
  const sunday = '2026-04-19';
  const nextSunday = '2026-04-26';

  it('files an event under the week it starts in', () => {
    const evt = makeEvent({
      id: 'mon',
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });
    const buckets = bucketEventsByWeek([evt], ZONE);
    expect(buckets.get(sunday)?.map((e) => e.id)).toEqual(['mon']);
    expect(buckets.has(nextSunday)).toBe(false);
  });

  // A bar is drawn again in every row it crosses, so every one of those rows
  // has to be handed the event.
  it('files a multi-day event under every week it crosses', () => {
    const evt = makeEvent({
      id: 'trip',
      startAt: '2026-04-22T00:00:00+09:00',
      endAt: '2026-04-28T00:00:00+09:00',
    });
    const buckets = bucketEventsByWeek([evt], ZONE);
    expect([...buckets.keys()].sort()).toEqual([sunday, nextSunday]);
  });

  it('leaves a week with nothing in it out of the buckets', () => {
    const evt = makeEvent({
      startAt: '2026-04-20T10:00:00+09:00',
      endAt: '2026-04-20T11:00:00+09:00',
    });
    expect(bucketEventsByWeek([evt], ZONE).get('2026-05-03')).toBeUndefined();
  });

  it('reads an event in the grid zone, not its own', () => {
    // 09:00 Monday in Tokyo is 20:00 the Sunday before in New York, which is
    // the week before as well.
    const evt = makeEvent({
      id: 'meeting',
      timezone: 'Asia/Tokyo',
      startAt: '2026-04-20T09:00:00+09:00',
      endAt: '2026-04-20T10:00:00+09:00',
    });
    expect([...bucketEventsByWeek([evt], 'America/New_York').keys()]).toEqual(['2026-04-19']);
  });

  it('keys buckets the way weekKey does', () => {
    const evt = makeEvent({
      startAt: '2026-04-22T10:00:00+09:00',
      endAt: '2026-04-22T11:00:00+09:00',
    });
    const day = DateTime.fromISO('2026-04-22T00:00:00', { zone: ZONE });
    expect(bucketEventsByWeek([evt], ZONE).has(weekKey(day))).toBe(true);
  });
});

describe('weekKeysInRange', () => {
  it('covers the weeks a landing range crosses and no others', () => {
    const start = DateTime.fromISO('2026-04-22T00:00:00', { zone: ZONE }); // Wednesday
    const end = start.plus({ days: 5 }); // the following Monday
    expect([...weekKeysInRange(start, end)].sort()).toEqual(['2026-04-19', '2026-04-26']);
  });

  it('covers one week when the range stays inside it', () => {
    const start = DateTime.fromISO('2026-04-20T00:00:00', { zone: ZONE });
    expect([...weekKeysInRange(start, start.plus({ days: 2 }))]).toEqual(['2026-04-19']);
  });
});

describe('layoutDayCell', () => {
  const weekStart = DateTime.fromISO('2026-04-19T00:00:00', { zone: ZONE }); // Sunday

  // Three short bars crowd Sunday through Wednesday; a week-long bar runs
  // underneath them. By Friday the short ones have ended and the long one is
  // the only event left on the day.
  const shortBars = ['s1', 's2', 's3'].map((id) =>
    makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-23T00:00:00+09:00' }),
  );
  const longBar = makeEvent({
    id: 'long',
    title: 'Long trip',
    startAt: '2026-04-19T00:00:00+09:00',
    endAt: '2026-04-26T00:00:00+09:00',
  });

  it('draws the long bar on a day whose crowd has ended', () => {
    const positioned = layoutWeek(weekStart, [...shortBars, longBar], ZONE);
    const friday = layoutDayCell(5, positioned, []);

    const drawn = positioned.filter(
      (p) => friday.reserved.includes(p.track) && p.track < MAX_VISIBLE_TRACKS,
    );
    expect(drawn.map((p) => p.event.id)).toContain('long');
    expect(friday.overflow).toBe(0);
  });

  it('counts the crowded days that lose a bar', () => {
    const positioned = layoutWeek(weekStart, [...shortBars, longBar], ZONE);
    // Four bars overlap on Sunday and only three tracks are drawn.
    expect(layoutDayCell(0, positioned, []).overflow).toBe(1);
  });

  it('reports a bar the cell cannot draw as overflow rather than nothing', () => {
    // Three bars run Sunday to Thursday and a fourth starts on that Thursday, so
    // it can only take the fourth track -- but it alone occupies Friday.
    const crowd = ['a', 'b', 'c'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-24T00:00:00+09:00' }),
    );
    const late = makeEvent({
      id: 'late',
      startAt: '2026-04-23T00:00:00+09:00',
      endAt: '2026-04-26T00:00:00+09:00',
    });
    const positioned = layoutWeek(weekStart, [...crowd, late], ZONE);
    expect(positioned.find((p) => p.event.id === 'late')?.track).toBe(MAX_VISIBLE_TRACKS);

    const friday = layoutDayCell(5, positioned, []);
    expect(friday.reserved).toEqual([MAX_VISIBLE_TRACKS]);
    expect(friday.overflow).toBe(1);
  });

  it('never reports a negative overflow', () => {
    const positioned = layoutWeek(
      weekStart,
      [
        ...['a', 'b', 'c', 'd'].map((id) =>
          makeEvent({
            id,
            startAt: '2026-04-19T00:00:00+09:00',
            endAt: '2026-04-24T00:00:00+09:00',
          }),
        ),
        makeEvent({
          id: 'late',
          startAt: '2026-04-23T00:00:00+09:00',
          endAt: '2026-04-26T00:00:00+09:00',
        }),
      ],
      ZONE,
    );
    for (let col = 0; col < 7; col++) {
      expect(layoutDayCell(col, positioned, []).overflow).toBeGreaterThanOrEqual(0);
    }
  });

  it('fills the tracks the bars leave free with single-day chips', () => {
    const bar = makeEvent({
      id: 'bar',
      startAt: '2026-04-19T00:00:00+09:00',
      endAt: '2026-04-26T00:00:00+09:00',
    });
    const positioned = layoutWeek(weekStart, [bar], ZONE);
    const singles = ['x', 'y', 'z'].map((id) =>
      makeEvent({ id, startAt: '2026-04-24T10:00:00+09:00', endAt: '2026-04-24T11:00:00+09:00' }),
    );
    const friday = layoutDayCell(5, positioned, singles);

    // The bar holds track 0, so the chips start at track 1 and the third is cut.
    expect(friday.singleSlots.map((s) => s.track)).toEqual([1, 2]);
    expect(friday.overflow).toBe(1);
  });
});
