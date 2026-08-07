import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  errorMessage: (e: unknown) => String(e),
}));

const deleteCalendar = vi.fn().mockResolvedValue(undefined);

const calendarState = {
  calendars: [
    { id: 'cal-owned', name: 'Family', color: '#47B2F7', role: 'owner', publicShared: false },
    { id: 'cal-joined', name: 'Work', color: '#F7A047', role: 'editor', publicShared: false },
  ],
  activeCalendarIds: ['cal-owned', 'cal-joined'],
  toggleCalendarFilter: vi.fn(),
  setActiveCalendarIds: vi.fn(),
  addCalendar: vi.fn(),
  deleteCalendar,
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { CalendarList } from './calendar-list';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('CalendarList calendar deletion', () => {
  // Deleting takes the calendar's events, memos and album with it for every
  // member, which is the whole reason the trash icon is not the delete.
  it('asks before deleting, then deletes the calendar it was pressed on', async () => {
    const user = userEvent.setup();
    render(<CalendarList />);

    await user.click(screen.getByRole('button', { name: 'calendar.deleteCalendar' }));
    expect(deleteCalendar).not.toHaveBeenCalled();
    expect(screen.getByText('calendar.deleteWarning')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'common.delete' }));
    expect(deleteCalendar).toHaveBeenCalledWith('cal-owned');
  });

  it('leaves the calendar alone when the confirmation is dismissed', async () => {
    const user = userEvent.setup();
    render(<CalendarList />);

    await user.click(screen.getByRole('button', { name: 'calendar.deleteCalendar' }));
    await user.click(screen.getByRole('button', { name: 'common.cancel' }));

    expect(deleteCalendar).not.toHaveBeenCalled();
    expect(screen.queryByText('calendar.deleteWarning')).toBeNull();
  });

  // Only the owner may delete; an editor's calendar offers no way to try.
  it('offers deletion on the owned calendar only', () => {
    render(<CalendarList />);

    expect(screen.getAllByRole('button', { name: 'calendar.deleteCalendar' })).toHaveLength(1);
  });
});
