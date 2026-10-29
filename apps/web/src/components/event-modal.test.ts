import { DateTime } from 'luxon';
import { beforeAll, describe, expect, it } from 'vitest';
import { presetToRule, rebuildPresetRule, ruleToPreset } from '@/lib/recurrence-preset';

type EventModalExports = typeof import('./event-modal');

let clampRecurrenceUntilDate: EventModalExports['clampRecurrenceUntilDate'];
let eventCommentText: EventModalExports['eventCommentText'];
let recurrenceUntilDateValue: EventModalExports['recurrenceUntilDateValue'];

beforeAll(async () => {
  const store = new Map<string, string>();
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (key: string) => store.get(key) ?? null,
      setItem: (key: string, value: string) => store.set(key, value),
      removeItem: (key: string) => store.delete(key),
      clear: () => store.clear(),
    },
    configurable: true,
  });
  ({ clampRecurrenceUntilDate, eventCommentText, recurrenceUntilDateValue } = await import(
    './event-modal'
  ));
});

describe('event comment API contract', () => {
  it('reads the backend body field', () => {
    expect(
      eventCommentText({
        id: 'comment-1',
        body: 'Visible comment',
        userName: 'Alice',
        userAvatar: 'A',
        userPublicId: 'user-1',
        createdAt: '2026-07-16T00:00:00Z',
      }),
    ).toBe('Visible comment');
  });
});

describe('event modal recurrence helpers', () => {
  it('recognizes non-preset recurrence rules as custom', () => {
    const preset = ruleToPreset(
      { freq: 'weekly', interval: 2, byDay: ['MO', 'WE'] },
      DateTime.fromISO('2026-07-06T09:00:00'),
    );

    expect(preset).toBe('custom');
  });

  it('rebuilds start-date-derived weekly rules from the new start date', () => {
    const rule = presetToRule('weekly', DateTime.fromISO('2026-07-07T09:00:00'));

    expect(rule).toMatchObject({ freq: 'weekly', interval: 1, byDay: ['TU'] });
  });

  // The calendar grid starts its weeks on Sunday, and wkst is what makes a
  // fortnightly rule count weeks the way the grid draws them.
  it('writes the Sunday week start on weekly presets', () => {
    const start = DateTime.fromISO('2026-07-07T09:00:00');

    expect(presetToRule('weekly', start)).toEqual({
      freq: 'weekly',
      interval: 1,
      byDay: ['TU'],
      wkst: 'SU',
    });
    expect(presetToRule('weekdays', start)).toMatchObject({ wkst: 'SU' });
    expect(presetToRule('daily', start)).not.toHaveProperty('wkst');
    expect(presetToRule('monthly_date', start)).not.toHaveProperty('wkst');
  });

  it('writes monthly dates as arrays', () => {
    expect(presetToRule('monthly_date', DateTime.fromISO('2026-07-21T09:00:00'))).toEqual({
      freq: 'monthly',
      interval: 1,
      byMonthDay: [21],
    });
  });

  it('reads canonical array rules back as the presets that wrote them', () => {
    const start = DateTime.fromISO('2026-07-21T09:00:00');

    expect(ruleToPreset({ freq: 'monthly', interval: 1, byMonthDay: [21] }, start)).toBe(
      'monthly_date',
    );
    expect(
      ruleToPreset({ freq: 'monthly', interval: 1, bySetPos: [3], byDay: ['TU'] }, start),
    ).toBe('monthly_nth');
    expect(ruleToPreset({ freq: 'monthly', interval: 1, byMonthDay: [1, 15] }, start)).toBe(
      'custom',
    );
    expect(ruleToPreset({ freq: 'weekly', interval: 1, byDay: ['TU'], wkst: 'SU' }, start)).toBe(
      'weekly',
    );
  });

  // A rule saved with Monday weeks and interval > 1 names different weeks than
  // the same rule with Sunday weeks, so a date change must not swap one for the other.
  it('keeps the stored week start when a preset is rebuilt', () => {
    const rebuilt = rebuildPresetRule('weekly', DateTime.fromISO('2026-07-08T09:00:00'), {
      freq: 'weekly',
      interval: 1,
      byDay: ['TU'],
      wkst: 'MO',
    });

    expect(rebuilt).toMatchObject({ byDay: ['WE'], wkst: 'MO' });
  });

  it('rebuilds start-date-derived monthly nth rules from the new start date', () => {
    const rule = presetToRule('monthly_nth', DateTime.fromISO('2026-07-21T09:00:00'));

    expect(rule).toMatchObject({ freq: 'monthly', interval: 1, bySetPos: [3], byDay: ['TU'] });
  });

  it('parses recurrence until dates in the event timezone', () => {
    const date = recurrenceUntilDateValue('2026-07-02', 'America/New_York');

    expect(date.zoneName).toBe('America/New_York');
    expect(date.toFormat('yyyy-MM-dd')).toBe('2026-07-02');
  });

  it('clamps recurrence until before the start date', () => {
    const date = clampRecurrenceUntilDate(
      DateTime.fromISO('2026-07-01T00:00:00', { zone: 'Asia/Tokyo' }),
      '2026-07-02T09:00:00',
      'Asia/Tokyo',
    );

    expect(date.toFormat('yyyy-MM-dd')).toBe('2026-07-02');
  });
});
