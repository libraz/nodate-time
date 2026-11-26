import { act, cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => unknown) => selector({ locale: 'en' }),
}));

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: 'me' } }),
}));

import { EMPTY_FILTER } from '@/lib/event-filter';
import { useCalendarStore } from '@/stores/calendar-store';
import type { Member } from '@/types/calendar';
import { EventFilterButton } from './event-filter-button';
import { useFilterPanel } from './event-filter-panel';

const member = (id: string, name: string, color: string): Member =>
  ({ id, name, color, role: 'editor' }) as Member;

beforeEach(() => {
  localStorage.clear();
  useCalendarStore.setState({
    activeCalendarIds: ['cal-1'],
    membersMap: {
      'cal-1': [
        member('a', 'Aiko', '#e11'),
        member('me', 'Mine', '#1e1'),
        member('b', 'Ben', '#11e'),
      ],
    },
    memberErrors: {},
    eventFilter: EMPTY_FILTER,
  });
});

afterEach(() => {
  cleanup();
  act(() => useFilterPanel.getState().close());
  useCalendarStore.setState({ eventFilter: EMPTY_FILTER, membersMap: {}, activeCalendarIds: [] });
});

async function openPanel() {
  const user = userEvent.setup();
  render(<EventFilterButton variant="desktop" />);
  await user.click(screen.getByRole('button', { name: 'Filter events' }));
  return { user, dialog: screen.getByRole('dialog', { name: 'Filter' }) };
}

describe('EventFilterPanel', () => {
  it('opens as a modal dialog with the people already listed, signed-in user first', async () => {
    const { dialog } = await openPanel();
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    const people = within(dialog)
      .getAllByRole('button', { pressed: false })
      .map((b) => b.querySelector('.truncate')?.textContent);
    expect(people).toEqual(['Me', 'Aiko', 'Ben']);
  });

  it('filters on the first tap, by who is involved', async () => {
    const { user, dialog } = await openPanel();
    expect(useCalendarStore.getState().eventFilter.conditions).toEqual([]);

    await user.click(within(dialog).getByRole('button', { name: 'Aiko' }));

    const [row] = useCalendarStore.getState().eventFilter.conditions;
    expect(row).toMatchObject({
      field: 'involved',
      exclude: false,
      people: [{ id: 'a', name: 'Aiko' }],
    });
  });

  it('switches the field and to excluding, and keeps an empty draft out of storage', async () => {
    const { user, dialog } = await openPanel();
    await user.click(within(dialog).getByRole('radio', { name: 'Created by' }));
    expect(useCalendarStore.getState().eventFilter).toEqual(EMPTY_FILTER);
    expect(localStorage.getItem('tt_eventFilter')).toBeNull();

    await user.click(within(dialog).getByRole('button', { name: 'Ben' }));
    await user.click(within(dialog).getByRole('radio', { name: 'Except' }));

    const [row] = useCalendarStore.getState().eventFilter.conditions;
    expect(row).toMatchObject({ field: 'creator', exclude: true, people: [{ id: 'b' }] });
  });

  it('offers all/any only once two conditions name someone', async () => {
    const { user, dialog } = await openPanel();
    await user.click(within(dialog).getByRole('button', { name: 'Aiko' }));
    await user.click(within(dialog).getByRole('button', { name: /Add condition/ }));
    expect(within(dialog).queryByRole('radiogroup', { name: 'Show events that match' })).toBeNull();

    const second = within(dialog).getByRole('region', { name: 'Condition 2' });
    await user.click(within(second).getByRole('button', { name: 'Ben' }));
    await user.click(within(dialog).getByRole('radio', { name: 'any' }));

    expect(useCalendarStore.getState().eventFilter.match).toBe('any');
    expect(useCalendarStore.getState().eventFilter.conditions).toHaveLength(2);
  });

  it('clears everything in one step', async () => {
    const { user, dialog } = await openPanel();
    await user.click(within(dialog).getByRole('button', { name: 'Aiko' }));
    await user.click(within(dialog).getByRole('button', { name: 'Clear all' }));
    expect(useCalendarStore.getState().eventFilter).toEqual(EMPTY_FILTER);
  });

  it('says when no calendar is shown instead of listing nobody', async () => {
    useCalendarStore.setState({ activeCalendarIds: [] });
    const { dialog } = await openPanel();
    expect(within(dialog).getByText('No calendars are shown')).toBeInTheDocument();
  });
});
