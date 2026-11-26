import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { makeEvent as makeFullEvent } from '@/test/make-event';
import type { CalendarEvent } from '@/types/calendar';
import {
  bucketEventsByDay,
  bucketEventsByWeek,
  dayKey,
  eventOccupiesDay,
  eventStartDay,
  isMultiDay,
  layoutDayCell,
  layoutWeek,
  MIN_TRACKS,
  weekKey,
  weekKeysInRange,
  weekTrackCount,
} from './week-layout';

const ZONE = 'Asia/Tokyo';

function makeEvent(
  overrides: Partial<CalendarEvent> & { startAt: string; endAt: string },
): CalendarEvent {
  return makeFullEvent({
    id: overrides.id ?? Math.random().toString(36).slice(2),
    title: overrides.title ?? 'Event',
    ...overrides,
  });
}

// Share and embed pages place published events with the same helper as the
// signed-in grid, so one event lands on the same day in every view.
describe('eventOccupiesDay for a published event', () => {
  const tokyo = (iso: string) => DateTime.fromISO(iso, { zone: 'Asia/Tokyo' });

  it('places a timed event on the viewer’s day, not the event zone’s', () => {
    const lateCall = {
      startAt: '2026-04-01T23:00:00-05:00',
      endAt: '2026-04-01T23:30:00-05:00',
      timezone: 'America/Chicago',
      allDay: false,
    };

    expect(eventOccupiesDay(lateCall, tokyo('2026-04-02'), 'Asia/Tokyo')).toBe(true);
    expect(eventOccupiesDay(lateCall, tokyo('2026-04-01'), 'Asia/Tokyo')).toBe(false);
  });

  it('keeps an all-day event on its own dates', () => {
    const holiday = {
      startAt: '2026-04-01T00:00:00Z',
      endAt: '2026-04-02T00:00:00Z',
      startDate: '2026-04-01',
      endDate: '2026-04-02',
      allDay: true,
    };

    expect(eventOccupiesDay(holiday, tokyo('2026-04-01'), 'Asia/Tokyo')).toBe(true);
    expect(eventOccupiesDay(holiday, tokyo('2026-04-02'), 'Asia/Tokyo')).toBe(false);
  });
});

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
      startAt: '2026-04-20T00:00:00Z',
      endAt: '2026-04-21T00:00:00Z',
      startDate: '2026-04-20',
      endDate: '2026-04-21',
    });
    expect(isMultiDay(evt, ZONE)).toBe(false);
  });

  it('is true for a two-day all-day event (exclusive end)', () => {
    const evt = makeEvent({
      allDay: true,
      startAt: '2026-04-20T00:00:00Z',
      endAt: '2026-04-22T00:00:00Z',
      startDate: '2026-04-20',
      endDate: '2026-04-22',
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
    startAt: '2026-08-05T00:00:00Z',
    endAt: '2026-08-06T00:00:00Z',
    startDate: '2026-08-05',
    endDate: '2026-08-06',
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
      startAt: '2026-04-20T00:00:00Z',
      endAt: '2026-04-21T00:00:00Z',
      startDate: '2026-04-20',
      endDate: '2026-04-21',
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

  it('keeps an empty week three tracks tall', () => {
    expect(MIN_TRACKS).toBe(3);
  });

  it('gives the longest bar the lowest track when several start together', () => {
    // Three short bars listed before the long one: on plain first-fit the long
    // bar lands on the deepest track, pushing the whole row taller.
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

    const drawn = positioned.filter((p) => friday.reserved.includes(p.track));
    expect(drawn.map((p) => p.event.id)).toEqual(['long']);
    expect(friday).not.toHaveProperty('overflow');
  });

  it('reserves a track for every bar on a crowded day', () => {
    const positioned = layoutWeek(weekStart, [...shortBars, longBar], ZONE);
    // Four bars overlap on Sunday and every one keeps its track.
    expect([...layoutDayCell(0, positioned, []).reserved].sort()).toEqual([0, 1, 2, 3]);
  });

  it('reserves the fourth track for a bar that alone occupies the day', () => {
    // Three bars run Sunday to Thursday and a fourth starts on that Thursday, so
    // it takes the fourth track -- and it alone occupies Friday.
    const crowd = ['a', 'b', 'c'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-24T00:00:00+09:00' }),
    );
    const late = makeEvent({
      id: 'late',
      startAt: '2026-04-23T00:00:00+09:00',
      endAt: '2026-04-26T00:00:00+09:00',
    });
    const positioned = layoutWeek(weekStart, [...crowd, late], ZONE);
    expect(positioned.find((p) => p.event.id === 'late')?.track).toBe(3);

    const friday = layoutDayCell(5, positioned, []);
    expect(friday.reserved).toEqual([3]);
  });

  it('places five single-day chips beside four bars, each on its own track', () => {
    const bars = ['a', 'b', 'c', 'd'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-24T00:00:00+09:00' }),
    );
    const positioned = layoutWeek(weekStart, bars, ZONE);
    const singles = ['v', 'w', 'x', 'y', 'z'].map((id) =>
      makeEvent({ id, startAt: '2026-04-21T10:00:00+09:00', endAt: '2026-04-21T11:00:00+09:00' }),
    );
    const tuesday = layoutDayCell(2, positioned, singles);

    expect([...tuesday.reserved].sort()).toEqual([0, 1, 2, 3]);
    expect(tuesday.singleSlots.map((s) => s.track)).toEqual([4, 5, 6, 7, 8]);
    expect(tuesday.singleSlots.map((s) => s.evt.id).sort()).toEqual(['v', 'w', 'x', 'y', 'z']);
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

    // The bar holds track 0, so the chips start at track 1 and none is cut.
    expect(friday.singleSlots.map((s) => s.track)).toEqual([1, 2, 3]);
  });

  it('orders single-day chips all-day first and then by start, whatever the input order', () => {
    const at = (id: string, hour: string, allDay = false) =>
      makeEvent({
        id,
        allDay,
        startAt: `2026-04-24T${hour}:00+09:00`,
        endAt: `2026-04-24T${hour}:30+09:00`,
      });
    const singles = [at('ten', '10'), at('eight', '08'), at('day', '00', true), at('nine', '09')];

    const friday = layoutDayCell(5, [], singles);

    // Every chip is placed, the latest-starting one on the deepest track.
    expect(friday.singleSlots.map((s) => s.evt.id)).toEqual(['day', 'eight', 'nine', 'ten']);
    expect(friday.singleSlots.map((s) => s.track)).toEqual([0, 1, 2, 3]);
  });
});

describe('weekTrackCount', () => {
  const weekStart = DateTime.fromISO('2026-04-19T00:00:00', { zone: ZONE }); // Sunday
  const cellsOf = (positioned: ReturnType<typeof layoutWeek>, singles: CalendarEvent[][]) =>
    Array.from({ length: 7 }, (_, col) => layoutDayCell(col, positioned, singles[col] ?? []));

  it('keeps an empty week at the minimum', () => {
    expect(weekTrackCount(cellsOf([], []))).toBe(MIN_TRACKS);
  });

  it('keeps a week under the minimum at the minimum', () => {
    const one = makeEvent({
      startAt: '2026-04-21T10:00:00+09:00',
      endAt: '2026-04-21T11:00:00+09:00',
    });
    expect(weekTrackCount(cellsOf([], [[], [], [one]]))).toBe(MIN_TRACKS);
  });

  it('grows to the deepest single-day chip of any day', () => {
    const singles = Array.from({ length: 8 }, (_, i) =>
      makeEvent({
        id: `s${i}`,
        startAt: '2026-04-24T10:00:00+09:00',
        endAt: '2026-04-24T11:00:00+09:00',
      }),
    );
    expect(weekTrackCount(cellsOf([], [[], [], [], [], [], singles]))).toBe(8);
  });

  it('grows to the deepest bar even on a day with no chips', () => {
    const crowd = ['a', 'b', 'c'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-24T00:00:00+09:00' }),
    );
    const late = makeEvent({
      id: 'late',
      startAt: '2026-04-23T00:00:00+09:00',
      endAt: '2026-04-26T00:00:00+09:00',
    });
    const positioned = layoutWeek(weekStart, [...crowd, late], ZONE);
    expect(weekTrackCount(cellsOf(positioned, []))).toBe(4);
  });

  it('counts chips stacked under bars', () => {
    const bars = ['a', 'b', 'c', 'd'].map((id) =>
      makeEvent({ id, startAt: '2026-04-19T00:00:00+09:00', endAt: '2026-04-24T00:00:00+09:00' }),
    );
    const positioned = layoutWeek(weekStart, bars, ZONE);
    const singles = ['v', 'w', 'x', 'y', 'z'].map((id) =>
      makeEvent({ id, startAt: '2026-04-21T10:00:00+09:00', endAt: '2026-04-21T11:00:00+09:00' }),
    );
    expect(weekTrackCount(cellsOf(positioned, [[], [], singles]))).toBe(9);
  });
});

describe('bucketEventsByDay', () => {
  const first = DateTime.fromISO('2026-03-01', { zone: ZONE });
  const last = DateTime.fromISO('2026-03-31', { zone: ZONE });

  it('places each event on the same days eventOccupiesDay accepts', () => {
    const events = [
      makeEvent({ startAt: '2026-03-10T01:00:00+09:00', endAt: '2026-03-10T02:00:00+09:00' }),
      makeEvent({ startAt: '2026-03-12T10:00:00+09:00', endAt: '2026-03-14T10:00:00+09:00' }),
      makeEvent({
        allDay: true,
        timezone: ZONE,
        startAt: '2026-03-20T00:00:00Z',
        endAt: '2026-03-22T00:00:00Z',
        startDate: '2026-03-20',
        endDate: '2026-03-22',
      }),
      makeEvent({ startAt: '2026-02-27T10:00:00+09:00', endAt: '2026-03-02T10:00:00+09:00' }),
    ];
    const buckets = bucketEventsByDay(events, ZONE, first, last);

    for (let d = 0; d < 31; d++) {
      const day = first.plus({ days: d });
      const expected = events.filter((e) => eventOccupiesDay(e, day, ZONE));
      expect(buckets.get(dayKey(day, ZONE)) ?? []).toEqual(expected);
    }
  });

  it('does not walk days outside the range', () => {
    const events = [
      makeEvent({ startAt: '2026-01-01T00:00:00+09:00', endAt: '2026-12-31T00:00:00+09:00' }),
    ];
    expect(bucketEventsByDay(events, ZONE, first, last).size).toBe(31);
  });
});
