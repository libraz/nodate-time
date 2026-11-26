import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => unknown) => selector({ locale: 'en' }),
}));

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: 'me' } }),
}));

import { EMPTY_FILTER, type EventFilter, type FilterCondition } from '@/lib/event-filter';
import { useCalendarStore } from '@/stores/calendar-store';
import type { Member } from '@/types/calendar';
import { EventFilterBar } from './event-filter-bar';

const notBen: FilterCondition = {
  id: 'c2',
  field: 'participant',
  exclude: true,
  people: [{ id: 'b', name: 'Ben' }],
};

const owners: EventFilter = {
  match: 'all',
  conditions: [
    {
      id: 'c1',
      field: 'owner',
      exclude: false,
      people: [
        { id: 'a', name: 'Old name' },
        { id: 'me', name: 'Mine' },
      ],
    },
    notBen,
  ],
};

afterEach(() => {
  cleanup();
  useCalendarStore.setState({ eventFilter: EMPTY_FILTER, membersMap: {}, activeCalendarIds: [] });
});

describe('EventFilterBar', () => {
  it('is absent while nothing is filtered', () => {
    render(<EventFilterBar />);
    expect(screen.queryByText('Filtered')).toBeNull();
  });

  it('summarises the first condition with live names and counts the rest', () => {
    useCalendarStore.setState({
      eventFilter: owners,
      activeCalendarIds: ['cal-1'],
      membersMap: { 'cal-1': [{ id: 'a', name: 'Aiko', color: '#e11', role: 'editor' } as Member] },
    });
    render(<EventFilterBar />);
    expect(screen.getByText('Filtered')).toBeInTheDocument();
    expect(screen.getByText(/Whose: Aiko, Me/)).toBeInTheDocument();
    expect(screen.getByText('+1 more')).toBeInTheDocument();
  });

  it('words an excluding condition as such', () => {
    useCalendarStore.setState({
      eventFilter: { match: 'all', conditions: [notBen] },
    });
    render(<EventFilterBar />);
    expect(screen.getByText(/Participant: not Ben/)).toBeInTheDocument();
  });

  it('clears the filter', async () => {
    useCalendarStore.setState({ eventFilter: owners });
    render(<EventFilterBar />);
    await userEvent.click(screen.getByRole('button', { name: 'Clear' }));
    expect(useCalendarStore.getState().eventFilter).toEqual(EMPTY_FILTER);
    expect(screen.queryByText('Filtered')).toBeNull();
  });
});
