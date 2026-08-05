import { describe, expect, it } from 'vitest';
import { eventTimesForSave } from './event-times';

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
