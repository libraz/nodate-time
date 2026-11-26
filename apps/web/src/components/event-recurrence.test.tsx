import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { DateTime } from 'luxon';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { WeekStart } from '@/lib/date-utils';
import type { RecurrencePreset, RecurrenceRule } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = { locale: 'en', timezone: 'Asia/Tokyo', weekStart: 'sun' as WeekStart };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { RecurrenceEditor, type RecurrenceValue } from './event-recurrence';

afterEach(() => {
  cleanup();
  uiState.weekStart = 'sun';
});

const START = DateTime.fromISO('2026-07-07T09:00:00', { zone: 'Asia/Tokyo' }); // Tuesday

function renderEditor(preset: RecurrencePreset, rule: RecurrenceRule | null) {
  const onChange = vi.fn<(update: (c: RecurrenceValue) => RecurrenceValue) => void>();
  render(
    <RecurrenceEditor
      preset={preset}
      rule={rule}
      start={START}
      zone="Asia/Tokyo"
      showSummary={false}
      onChange={onChange}
    />,
  );
  /** The value the last change produces from `from`. */
  const applied = (from: RecurrenceValue) => onChange.mock.calls.at(-1)?.[0](from);
  return { applied };
}

const weekdayButtons = () =>
  screen
    .getAllByRole('button', { name: /^(Sun|Mon|Tue|Wed|Thu|Fri|Sat)$/ })
    .map((el) => el.textContent);

describe('RecurrenceEditor week start', () => {
  const weekly: RecurrenceRule = { freq: 'weekly', interval: 1, byDay: ['TU', 'TH'] };

  it.each([
    ['sun', ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']],
    ['mon', ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']],
    ['sat', ['Sat', 'Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri']],
  ] as const)('orders the weekday buttons from a %s start', (ws, labels) => {
    uiState.weekStart = ws;
    renderEditor('custom', weekly);

    expect(weekdayButtons()).toEqual(labels);
  });

  it('still toggles the weekday a button names', () => {
    uiState.weekStart = 'mon';
    const { applied } = renderEditor('custom', weekly);

    fireEvent.click(screen.getByRole('button', { name: 'Sun' }));

    expect(applied({ preset: 'custom', rule: weekly })?.rule?.byDay).toEqual(['TU', 'TH', 'SU']);
  });

  it.each([
    ['mon', 'MO'],
    ['sun', 'SU'],
    ['sat', 'SU'],
  ] as const)('writes wkst %2$s on a weekly preset picked with a %1$s start', (ws, wkst) => {
    uiState.weekStart = ws;
    const { applied } = renderEditor('none', null);

    fireEvent.click(screen.getByRole('button', { name: 'event.recurrence' }));
    fireEvent.click(screen.getByRole('option', { name: 'event.recurrenceWeekly' }));

    expect(applied({ preset: 'none', rule: null })?.rule).toMatchObject({
      freq: 'weekly',
      byDay: ['TU'],
      wkst,
    });
  });

  it('writes the week start when a custom rule switches to weekly', () => {
    uiState.weekStart = 'mon';
    const { applied } = renderEditor('custom', { freq: 'daily', interval: 2 });

    fireEvent.click(screen.getByRole('button', { name: 'event.recurrenceInterval' }));
    fireEvent.click(screen.getByRole('option', { name: 'event.unitWeek' }));

    expect(applied({ preset: 'custom', rule: { freq: 'daily', interval: 2 } })?.rule).toMatchObject(
      { freq: 'weekly', byDay: ['TU'], wkst: 'MO' },
    );
  });
});
