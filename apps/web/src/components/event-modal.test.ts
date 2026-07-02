import { DateTime } from 'luxon';
import { beforeAll, describe, expect, it } from 'vitest';

type EventModalExports = typeof import('./event-modal');

let clampRecurrenceUntilDate: EventModalExports['clampRecurrenceUntilDate'];
let presetToRule: EventModalExports['presetToRule'];
let recurrenceUntilDateValue: EventModalExports['recurrenceUntilDateValue'];
let ruleToPreset: EventModalExports['ruleToPreset'];

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
  ({ clampRecurrenceUntilDate, presetToRule, recurrenceUntilDateValue, ruleToPreset } =
    await import('./event-modal'));
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

  it('rebuilds start-date-derived monthly nth rules from the new start date', () => {
    const rule = presetToRule('monthly_nth', DateTime.fromISO('2026-07-21T09:00:00'));

    expect(rule).toMatchObject({ freq: 'monthly', interval: 1, bySetPos: 3, byDay: ['TU'] });
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
