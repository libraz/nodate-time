import { DateTime } from 'luxon';
import type { SchedulePollCandidate } from '@/lib/schedule-polls';

/** Candidate counts offered by the automatic mode. */
export const AUTO_PICK_COUNTS = [3, 5, 8] as const;
export type AutoPickCount = (typeof AUTO_PICK_COUNTS)[number];

type Entry = {
  candidate: SchedulePollCandidate;
  start: number;
  end: number;
  day: number;
  weekday: number;
  band: number;
  minute: number;
  /** 1 at the middle of the searched hours, 0 at either edge. */
  centrality: number;
  open: boolean;
  /** Starts within a day, too short notice for most invitees. */
  soon: boolean;
  /** Overlaps the 12:00-13:00 lunch break. */
  lunch: boolean;
};

const LUNCH_START = 12 * 60;
const LUNCH_END = 13 * 60;

// Picks stay within this many days of the first free day unless too few slots fit there.
const horizonDays = (count: number) => Math.max(7, count * 2);

const score = (entry: Entry, picked: Entry[], firstDay: number, span: number) => {
  let value = entry.open ? 1 : 0.35;
  let nearestGap = Number.POSITIVE_INFINITY;
  for (const other of picked) {
    const gap = Math.abs(entry.day - other.day);
    nearestGap = Math.min(nearestGap, gap);
    if (gap === 0) value -= 0.6;
    else if (gap === 1) value -= 0.2;
    if (entry.weekday === other.weekday) value -= 0.12;
    if (entry.band === other.band) value -= 0.18;
    if (entry.minute === other.minute) value -= 0.08;
  }
  // The first and last slot of the searched hours are the hardest for people to reach.
  value += 0.06 * entry.centrality;
  // Round start times are easier to agree on than quarter-past ones.
  if (entry.minute % 60 === 0) value += 0.12;
  else if (entry.minute % 30 === 0) value += 0.06;
  if (entry.lunch) value -= 0.5;
  // Spacing beyond two days buys nothing, so the spread never pushes picks further out.
  if (picked.length > 0) value += (0.15 * Math.min(nearestGap, 2)) / 2;
  // A sooner date is easier to commit to.
  return value - (0.3 * (entry.day - firstDay)) / span;
};

const overlapsLunch = (start: DateTime, end: DateTime) => {
  const from = start.hour * 60 + start.minute;
  const to = from + end.diff(start, 'minutes').minutes;
  // Slots last at most a day, so the next day's lunch is the only other one in reach.
  return [0, 1440].some((day) => from < day + LUNCH_END && day + LUNCH_START < to);
};

/**
 * Picks `count` candidates likely to reach agreement: free-for-everyone slots first, spread
 * across days, weekdays and times of day so each invitee has some option that suits them, but
 * kept near the first free day so the meeting does not drift far out. Overlapping slots are
 * never picked together. The result is ordered by start time.
 */
export function autoPickCandidates(
  suggestions: SchedulePollCandidate[],
  count: number,
  timezone: string,
  now: number = Date.now(),
): SchedulePollCandidate[] {
  const entries: Entry[] = suggestions
    .filter((candidate) => candidate.active && candidate.availability !== 'hard')
    .map((candidate) => {
      const start = DateTime.fromISO(candidate.startAt, { setZone: true }).setZone(timezone);
      const end = DateTime.fromISO(candidate.endAt, { setZone: true });
      return {
        candidate,
        start: start.toMillis(),
        end: end.toMillis(),
        day: Math.round(start.startOf('day').toMillis() / 86_400_000),
        weekday: start.weekday,
        band: 0,
        minute: start.hour * 60 + start.minute,
        centrality: 0,
        open: candidate.availability === 'open',
        soon: start.toMillis() - now < 86_400_000,
        lunch: overlapsLunch(start, end),
      };
    })
    .sort((a, b) => a.start - b.start);
  if (entries.length === 0) return [];
  const minutes = entries.map((entry) => entry.minute);
  const earliest = Math.min(...minutes);
  const latest = Math.max(...minutes);
  for (const entry of entries) {
    // Thirds of the searched hours: people with fixed routines tend to be free in one of them.
    entry.band = Math.floor((3 * (entry.minute - earliest)) / (latest - earliest + 1));
    entry.centrality =
      latest === earliest
        ? 1
        : 1 - Math.abs(((entry.minute - earliest) / (latest - earliest)) * 2 - 1);
  }
  const later = entries.filter((entry) => !entry.soon);
  const firstDay = (later[0] ?? entries[0])?.day ?? 0;
  const near = later.filter((entry) => entry.day - firstDay < horizonDays(count));

  // Widen only when the narrower pool runs out: near days, then later days, then short notice.
  const picked: Entry[] = [];
  for (const pool of [near, later, entries]) {
    const span = Math.max(1, (pool[pool.length - 1]?.day ?? firstDay) - firstDay);
    pickFrom(pool, picked, count, firstDay, span);
  }
  return picked.sort((a, b) => a.start - b.start).map((entry) => entry.candidate);
}

function pickFrom(pool: Entry[], picked: Entry[], count: number, firstDay: number, span: number) {
  while (picked.length < count) {
    let best: Entry | undefined;
    let bestScore = Number.NEGATIVE_INFINITY;
    for (const entry of pool) {
      if (picked.includes(entry)) continue;
      if (picked.some((other) => entry.start < other.end && other.start < entry.end)) continue;
      const value = score(entry, picked, firstDay, span);
      if (value > bestScore) {
        best = entry;
        bestScore = value;
      }
    }
    if (!best) return;
    picked.push(best);
  }
}
