import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { eventTimesForSave, shiftStartKeepingDuration } from './event-times';

const TOKYO = 'Asia/Tokyo';
const NEW_YORK = 'America/New_York';

describe('eventTimesForSave', () => {
  // A member in New York opening a Tokyo all-day event and pressing save
  // without changing anything must not move it. Reading the dates in the
  // viewer's zone snaps them to New York midnight, which is 13 hours off the
  // instants the event is stored at — the event silently shifts a day.
  it('anchors an all-day event to its own zone, not the viewer’s', () => {
    const times = eventTimesForSave(
      { allDay: true, startAt: '2026-08-05T00:00', endAt: '2026-08-05T00:00' },
      NEW_YORK,
      TOKYO,
    );
    expect(times.startAt).toBe('2026-08-05T00:00:00.000+09:00');
    expect(times.endAt).toBe('2026-08-06T00:00:00.000+09:00');
  });

  it('stores the all-day end exclusively', () => {
    const times = eventTimesForSave(
      { allDay: true, startAt: '2026-08-05T00:00', endAt: '2026-08-07T00:00' },
      TOKYO,
      TOKYO,
    );
    expect(times.endAt).toBe('2026-08-08T00:00:00.000+09:00');
  });

  it('clamps an all-day end typed before its start', () => {
    const times = eventTimesForSave(
      { allDay: true, startAt: '2026-08-05T00:00', endAt: '2026-08-01T00:00' },
      TOKYO,
      TOKYO,
    );
    expect(times.endAt).toBe('2026-08-06T00:00:00.000+09:00');
  });

  // A timed event is an instant, and the editor shows it where the viewer is,
  // so that is where its fields have to be read back from.
  it('reads a timed event in the viewer’s zone', () => {
    const times = eventTimesForSave(
      { allDay: false, startAt: '2026-08-05T09:00', endAt: '2026-08-05T10:00' },
      NEW_YORK,
      TOKYO,
    );
    expect(times.startAt).toBe('2026-08-05T09:00:00.000-04:00');
    expect(times.endAt).toBe('2026-08-05T10:00:00.000-04:00');
  });

  it('gives a timed event an hour when its end is not after its start', () => {
    const times = eventTimesForSave(
      { allDay: false, startAt: '2026-08-05T09:00', endAt: '2026-08-05T09:00' },
      TOKYO,
      TOKYO,
    );
    expect(times.endAt).toBe('2026-08-05T10:00:00.000+09:00');
  });
});

describe('shiftStartKeepingDuration', () => {
  // New York springs forward on 2026-03-08. Every case below straddles that
  // date, so the wall-clock span and the elapsed milliseconds differ by an
  // hour — in a fixed-offset zone the two are the same number and neither
  // implementation can be told from the other.
  const day = (iso: string) => DateTime.fromISO(iso, { zone: NEW_YORK });

  it('keeps the wall-clock span when the new start lands after a spring-forward', () => {
    const moved = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-03-01T09:00', endAt: '2026-03-03T10:00' },
      day('2026-03-07'),
      NEW_YORK,
    );
    expect(moved.startAt).toBe('2026-03-07T09:00');
    // Two days and an hour later. Re-applying the 49 elapsed hours instead
    // lands on 11:00, an end the user never touched.
    expect(moved.endAt).toBe('2026-03-09T10:00');
  });

  it('keeps the wall-clock span when the old start sat across a spring-forward', () => {
    const moved = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-03-07T09:00', endAt: '2026-03-09T10:00' },
      day('2026-03-15'),
      NEW_YORK,
    );
    // The original span is 48 elapsed hours but two days and an hour on the
    // wall clock; carrying the elapsed figure over loses the hour.
    expect(moved.endAt).toBe('2026-03-17T10:00');
  });

  it('keeps the time of day the start already had', () => {
    const moved = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-03-01T22:30', endAt: '2026-03-02T00:30' },
      day('2026-03-20'),
      NEW_YORK,
    );
    expect(moved.startAt).toBe('2026-03-20T22:30');
    expect(moved.endAt).toBe('2026-03-21T00:30');
  });

  it('collapses an end typed before its start onto the new start', () => {
    const moved = shiftStartKeepingDuration(
      { allDay: false, startAt: '2026-03-05T09:00', endAt: '2026-03-04T09:00' },
      day('2026-03-10'),
      NEW_YORK,
    );
    expect(moved.endAt).toBe('2026-03-10T09:00');
  });
});
