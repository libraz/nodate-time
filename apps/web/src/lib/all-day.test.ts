import { describe, expect, it } from 'vitest';
// The shared contract's fixtures, read as text so no file access is needed.
import fixtureText from '../../../../sql/core/all-day/fixtures.json?raw';
import {
  allDayDates,
  allDayWireDates,
  toAllDayInclusiveEndInput,
  toLocalDatetimeInput,
} from './all-day';
import { eventEndDay, eventStartDay } from './week-layout';

describe('all-day event form dates', () => {
  it('converts exclusive stored end to inclusive editor end date', () => {
    expect(toAllDayInclusiveEndInput('2026-07-03T00:00:00+09:00', 'Asia/Tokyo')).toBe(
      '2026-07-02T00:00',
    );
  });

  it('keeps regular local datetime rendering unchanged', () => {
    expect(toLocalDatetimeInput('2026-07-02T10:30:00+09:00', 'Asia/Tokyo')).toBe(
      '2026-07-02T10:30',
    );
  });
});

interface FixtureView {
  firstDate: string;
  lastDate: string;
  startDate: string;
  endDate: string;
}

interface FixtureCase {
  name: string;
  kind: string;
  startAt: string;
  endAt: string;
  viewers: Record<string, FixtureView>;
}

const fixtures = JSON.parse(fixtureText) as { cases: FixtureCase[] };

// The dates an all-day row means are the same for every viewer, so each case
// is read once per zone and must give the same answer in all of them.
describe.each(fixtures.cases)('all-day fixture $name', (fixture) => {
  describe.each(Object.entries(fixture.viewers))('viewed from %s', (zone, expected) => {
    const wire = { startDate: expected.startDate, endDate: expected.endDate };

    it('reads the covered days from the wire dates', () => {
      expect(allDayDates(wire)).toEqual({
        firstDate: expected.firstDate,
        lastDate: expected.lastDate,
      });
    });

    it('lays the event on the same days in the grid', () => {
      const evt = { allDay: true, startAt: fixture.startAt, endAt: fixture.endAt, ...wire };

      expect(eventStartDay(evt, zone).toFormat('yyyy-MM-dd')).toBe(expected.firstDate);
      expect(eventEndDay(evt, zone).toFormat('yyyy-MM-dd')).toBe(expected.lastDate);
    });

    it('lays out a row that carries only its stored instants the same way', () => {
      const evt = { allDay: true, startAt: fixture.startAt, endAt: fixture.endAt };

      expect(eventStartDay(evt, zone).toFormat('yyyy-MM-dd')).toBe(expected.firstDate);
      expect(eventEndDay(evt, zone).toFormat('yyyy-MM-dd')).toBe(expected.lastDate);
      expect(allDayWireDates(evt)).toEqual(wire);
    });
  });
});
