import { DateTime } from 'luxon';
import { describe, expect, it } from 'vitest';
import { autoPickCandidates } from '@/lib/schedule-poll-auto-pick';
import type { SchedulePollCandidate } from '@/lib/schedule-polls';

const zone = 'Asia/Tokyo';
const base = DateTime.fromISO('2026-12-01T00:00', { zone });
const now = base.minus({ days: 3 }).toMillis();

const slot = (
  day: number,
  hour: number,
  availability: SchedulePollCandidate['availability'] = 'open',
  active = true,
): SchedulePollCandidate => {
  const start = base.plus({ days: day, hours: hour });
  return {
    startAt: start.toISO() ?? '',
    endAt: start.plus({ hours: 1 }).toISO() ?? '',
    availability,
    active,
  };
};

// Hourly 9:00-17:00 starts on each of `days` days, all free.
const grid = (days: number) =>
  Array.from({ length: days }, (_, day) =>
    Array.from({ length: 9 }, (_, i) => slot(day, 9 + i)),
  ).flat();

const local = (candidate: SchedulePollCandidate) =>
  DateTime.fromISO(candidate.startAt, { setZone: true }).setZone(zone);
const dayOf = (candidate: SchedulePollCandidate) =>
  Math.round(local(candidate).startOf('day').diff(base, 'days').days);

describe('autoPickCandidates', () => {
  it('spreads picks over distinct days and times of day', () => {
    const picks = autoPickCandidates(grid(14), 5, zone, now);
    expect(picks).toHaveLength(5);
    expect(new Set(picks.map(dayOf)).size).toBe(5);
    const bands = new Set(picks.map((c) => (local(c).hour < 12 ? 'am' : 'pm')));
    expect(bands.size).toBe(2);
  });

  it('keeps picks near the first free day instead of using the whole window', () => {
    const picks = autoPickCandidates(grid(30), 5, zone, now);
    expect(Math.max(...picks.map(dayOf))).toBeLessThan(10);
  });

  it('reaches past the near window only when too few slots fit there', () => {
    const suggestions = [slot(0, 10), slot(1, 10), slot(20, 10), slot(21, 14)];
    const picks = autoPickCandidates(suggestions, 3, zone, now);
    const days = picks.map(dayOf);
    expect(days.slice(0, 2)).toEqual([0, 1]);
    expect(days[2]).toBeGreaterThanOrEqual(20);
  });

  it('prefers slots free for everyone over ones that need adjusting', () => {
    const suggestions = [slot(0, 10, 'soft'), slot(1, 10, 'soft'), slot(2, 10), slot(3, 14)];
    const picks = autoPickCandidates(suggestions, 2, zone, now);
    expect(picks.every((c) => c.availability === 'open')).toBe(true);
  });

  it('never picks unavailable or overlapping slots', () => {
    const half = base.plus({ hours: 10, minutes: 30 });
    const overlapping: SchedulePollCandidate = {
      startAt: half.toISO() ?? '',
      endAt: half.plus({ hours: 1 }).toISO() ?? '',
      availability: 'open',
      active: true,
    };
    const suggestions = [slot(0, 10), overlapping, slot(1, 10, 'hard'), slot(2, 10, 'open', false)];
    const picks = autoPickCandidates(suggestions, 5, zone, now);
    expect(picks).toHaveLength(1);
  });

  it('avoids slots starting within a day while later ones are free', () => {
    const suggestions = [slot(0, 10), slot(0, 14), slot(2, 10)];
    const picks = autoPickCandidates(suggestions, 1, zone, base.toMillis());
    expect(picks.map(dayOf)).toEqual([2]);
  });

  it('prefers on-the-hour starts over quarter-past ones', () => {
    const quarter = (day: number) => {
      const start = base.plus({ days: day, hours: 11, minutes: 15 });
      return {
        ...slot(day, 11),
        startAt: start.toISO() ?? '',
        endAt: start.plus({ hours: 1 }).toISO() ?? '',
      };
    };
    const picks = autoPickCandidates([quarter(0), slot(0, 11), quarter(1)], 1, zone, now);
    expect(local(picks[0] as SchedulePollCandidate).minute).toBe(0);
  });

  it('keeps clear of the lunch break while other times are free', () => {
    const picks = autoPickCandidates(grid(14), 8, zone, now);
    for (const pick of picks) {
      const start = local(pick);
      expect(start.hour).not.toBe(12);
    }
  });

  it('returns picks in start order', () => {
    const picks = autoPickCandidates(grid(14), 8, zone, now);
    const starts = picks.map((c) => Date.parse(c.startAt));
    expect(starts).toEqual([...starts].sort((a, b) => a - b));
  });
});
