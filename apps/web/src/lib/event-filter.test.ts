import { describe, expect, it } from 'vitest';
import {
  EMPTY_FILTER,
  type EventFilter,
  type FilterCondition,
  type FilterField,
  filterCandidates,
  isFilterActive,
  matchesFilter,
  parseEventFilter,
  resolvePerson,
} from '@/lib/event-filter';
import { makeEvent } from '@/test/make-event';
import type { Member } from '@/types/calendar';

const A = { id: 'a', name: 'A' };
const B = { id: 'b', name: 'B' };

function cond(field: FilterField, people = [A], exclude = false): FilterCondition {
  return { id: `${field}-${exclude}`, field, exclude, people };
}

function only(...conditions: FilterCondition[]): EventFilter {
  return { match: 'all', conditions };
}

/** An event where A holds exactly one role, or none. */
const eventWhereA = {
  owner: makeEvent({ ownerId: 'a', creatorId: 'x', participantIds: ['y'] }),
  creator: makeEvent({ ownerId: 'x', creatorId: 'a', participantIds: ['y'] }),
  participant: makeEvent({ ownerId: 'x', creatorId: 'x', participantIds: ['y', 'a'] }),
  none: makeEvent({ ownerId: 'x', creatorId: 'x', participantIds: ['y'] }),
};

// Every field against every role A can hold. "Involved" is owner or participant;
// who created the event does not make it theirs.
const TRUTH: Record<FilterField, Record<keyof typeof eventWhereA, boolean>> = {
  involved: { owner: true, creator: false, participant: true, none: false },
  owner: { owner: true, creator: false, participant: false, none: false },
  creator: { owner: false, creator: true, participant: false, none: false },
  participant: { owner: false, creator: false, participant: true, none: false },
};

describe('matchesFilter', () => {
  for (const [field, row] of Object.entries(TRUTH) as [FilterField, typeof TRUTH.owner][]) {
    for (const [role, expected] of Object.entries(row) as [keyof typeof eventWhereA, boolean][]) {
      it(`${field} = A, A as ${role} → ${expected}`, () => {
        expect(matchesFilter(eventWhereA[role], only(cond(field)))).toBe(expected);
      });
      it(`${field} except A, A as ${role} → ${!expected}`, () => {
        expect(matchesFilter(eventWhereA[role], only(cond(field, [A], true)))).toBe(!expected);
      });
    }
  }

  it('matches any of several people, and excludes only when none of them match', () => {
    const ownedByB = makeEvent({ ownerId: 'b', participantIds: [] });
    expect(matchesFilter(ownedByB, only(cond('owner', [A, B])))).toBe(true);
    expect(matchesFilter(ownedByB, only(cond('owner', [A, B], true)))).toBe(false);
    expect(matchesFilter(ownedByB, only(cond('owner', [A], true)))).toBe(true);
  });

  it('treats a missing creator or participant list as nobody', () => {
    const bare = makeEvent({ ownerId: 'x', creatorId: '', participantIds: null });
    expect(matchesFilter(bare, only(cond('creator')))).toBe(false);
    expect(matchesFilter(bare, only(cond('participant')))).toBe(false);
    expect(matchesFilter(bare, only(cond('participant', [A], true)))).toBe(true);
  });

  it('shows everything when no row has anyone chosen', () => {
    expect(matchesFilter(eventWhereA.none, EMPTY_FILTER)).toBe(true);
    expect(matchesFilter(eventWhereA.none, only(cond('owner', [])))).toBe(true);
    expect(isFilterActive(only(cond('owner', [])))).toBe(false);
    expect(isFilterActive(only(cond('owner')))).toBe(true);
  });

  // Two rows, each passing, failing or empty (or absent), under both joins.
  type Row = 'pass' | 'fail' | 'empty' | 'absent';
  const row = (r: Row): FilterCondition[] =>
    r === 'absent' ? [] : [cond('owner', r === 'empty' ? [] : [r === 'pass' ? A : B], false)];
  const subject = makeEvent({ ownerId: 'a', participantIds: [] });
  const cases: [Row, Row, boolean, boolean][] = [
    // first, second, all, any
    ['pass', 'pass', true, true],
    ['pass', 'fail', false, true],
    ['pass', 'empty', true, true],
    ['pass', 'absent', true, true],
    ['fail', 'pass', false, true],
    ['fail', 'fail', false, false],
    ['fail', 'empty', false, false],
    ['fail', 'absent', false, false],
    ['empty', 'pass', true, true],
    ['empty', 'fail', false, false],
    ['empty', 'empty', true, true],
    ['empty', 'absent', true, true],
  ];
  for (const [first, second, all, any] of cases) {
    it(`${first} + ${second}: all → ${all}, any → ${any}`, () => {
      const conditions = [...row(first), ...row(second)].map((c, i) => ({ ...c, id: `r${i}` }));
      expect(matchesFilter(subject, { match: 'all', conditions })).toBe(all);
      expect(matchesFilter(subject, { match: 'any', conditions })).toBe(any);
    });
  }
});

describe('parseEventFilter', () => {
  const good = {
    match: 'any',
    conditions: [{ id: 'c1', field: 'creator', exclude: true, people: [{ id: 'a', name: 'A' }] }],
  };

  it('keeps a well-formed filter', () => {
    expect(parseEventFilter(good)).toEqual(good);
  });

  it.each([
    ['null', null],
    ['a string', 'all'],
    ['an unknown join', { ...good, match: 'none' }],
    ['conditions not a list', { ...good, conditions: {} }],
    ['an unknown field', { ...good, conditions: [{ ...good.conditions[0], field: 'label' }] }],
    ['exclude not a boolean', { ...good, conditions: [{ ...good.conditions[0], exclude: 'no' }] }],
    [
      'a person without a name',
      { ...good, conditions: [{ ...good.conditions[0], people: [{ id: 'a' }] }] },
    ],
    [
      'a numeric avatar',
      {
        ...good,
        conditions: [{ ...good.conditions[0], people: [{ id: 'a', name: 'A', avatarUrl: 1 }] }],
      },
    ],
  ])('reads %s as no filter', (_, raw) => {
    expect(parseEventFilter(raw)).toEqual(EMPTY_FILTER);
  });
});

describe('filterCandidates', () => {
  const m = (id: string, name: string): Member =>
    ({ id, name, color: '#000', role: 'editor' }) as Member;

  it('offers each person on a shown calendar once, signed-in user first', () => {
    const map = {
      c1: [m('a', 'A'), m('me', 'Me')],
      c2: [m('me', 'Me'), m('b', 'B')],
      c3: [m('z', 'Z')],
    };
    expect(filterCandidates(map, ['c1', 'c2'], 'me').map((p) => p.id)).toEqual(['me', 'a', 'b']);
  });

  it('offers nobody when no calendar is shown', () => {
    expect(filterCandidates({ c1: [m('a', 'A')] }, [], 'me')).toEqual([]);
  });
});

describe('resolvePerson', () => {
  it('uses the live name while the person is still offered, the kept one after', () => {
    const kept = { id: 'a', name: 'Old' };
    expect(resolvePerson(kept, [{ id: 'a', name: 'New' }]).name).toBe('New');
    expect(resolvePerson(kept, []).name).toBe('Old');
  });
});
