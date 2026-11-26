import { cleanup, render } from '@testing-library/react';
import { DateTime } from 'luxon';
import { createRef } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { WeekStart } from '@/lib/date-utils';

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useT: () => (key: string) => key,
}));

const uiState = { locale: 'en', holidaysCountry: null, weekStart: 'sun' as WeekStart };

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { DatePickerDropdown, type DateRangeLink } from './date-picker';

afterEach(() => {
  cleanup();
  uiState.weekStart = 'sun';
});

const ZONE = 'Asia/Tokyo';
// June 2026 starts on a Monday.
const VALUE = DateTime.fromISO('2026-06-10T00:00:00', { zone: ZONE });

function renderPicker(range?: DateRangeLink) {
  return render(
    <DatePickerDropdown
      value={VALUE}
      onChange={vi.fn()}
      onClose={vi.fn()}
      {...(range ? { range } : {})}
      style={{}}
      floatingRef={createRef<HTMLDivElement>()}
      returnFocusRef={createRef<HTMLElement>()}
    />,
  );
}

const headerLabels = () =>
  Array.from(document.querySelector('.grid.grid-cols-7')?.children ?? []).map(
    (el) => el.textContent,
  );

/** Blank cells before day 1 of the grid. */
function leadingBlanks(): number {
  const cells = Array.from(document.querySelector('.picker-turn')?.children ?? []);
  return cells.findIndex((el) => el.querySelector('[data-day]'));
}

describe('DatePickerDropdown week start', () => {
  it.each([
    ['sun', ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'], 1],
    ['mon', ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'], 0],
    ['sat', ['Sa', 'Su', 'Mo', 'Tu', 'We', 'Th', 'Fr'], 2],
  ] as const)('%s start: header %j with %i blanks before the 1st', (ws, labels, blanks) => {
    uiState.weekStart = ws;
    renderPicker();

    expect(headerLabels()).toEqual(labels);
    expect(leadingBlanks()).toBe(blanks);
  });

  it('keeps Sunday red and Saturday blue wherever their columns fall', () => {
    uiState.weekStart = 'mon';
    renderPicker();

    const cols = Array.from(document.querySelector('.grid.grid-cols-7')?.children ?? []);
    expect(cols[5]?.getAttribute('style')).toContain('var(--color-saturday)');
    expect(cols[6]?.getAttribute('style')).toContain('var(--color-sunday)');
    expect(cols[0]?.getAttribute('style')).toContain('var(--color-text-tertiary)');
  });
});

describe('DatePickerDropdown range band', () => {
  const range: DateRangeLink = {
    role: 'start',
    other: DateTime.fromISO('2026-06-17T00:00:00', { zone: ZONE }),
  };

  const bandOf = (iso: string) =>
    document
      .querySelector(`[data-day="${iso}"]`)
      ?.parentElement?.querySelector<HTMLElement>(':scope > span[aria-hidden]');

  // Monday 2026-06-15 opens a row on a Monday grid and sits mid-row on a Sunday one.
  it('rounds the band where a row opens, by the chosen week start', () => {
    uiState.weekStart = 'mon';
    renderPicker(range);

    expect(bandOf('2026-06-15')?.style.borderTopLeftRadius).toBe('999px');
    expect(bandOf('2026-06-14')?.style.borderTopRightRadius).toBe('999px');
  });

  it('does not round it mid-row', () => {
    uiState.weekStart = 'sun';
    renderPicker(range);

    expect(bandOf('2026-06-15')?.style.borderTopLeftRadius).not.toBe('999px');
    expect(bandOf('2026-06-14')?.style.borderTopLeftRadius).toBe('999px');
    expect(bandOf('2026-06-13')?.style.borderTopRightRadius).toBe('999px');
  });
});
