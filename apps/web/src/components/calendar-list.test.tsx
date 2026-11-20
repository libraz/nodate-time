import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Calendar } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  errorMessage: (e: unknown) => String(e),
}));

const deleteCalendar = vi.fn().mockResolvedValue(undefined);

const initialCalendars: Calendar[] = [
  {
    id: 'cal-owned',
    name: 'Family',
    color: '#47B2F7',
    createdAt: '',
    role: 'owner',
    publicShared: false,
    memberCount: 1,
    memberColor: '#47B2F7',
  },
  {
    id: 'cal-joined',
    name: 'Work',
    color: '#F7A047',
    createdAt: '',
    role: 'editor',
    publicShared: false,
    memberCount: 1,
    memberColor: '#F7A047',
  },
];

const calendarState = {
  calendars: [...initialCalendars],
  activeCalendarIds: ['cal-owned', 'cal-joined'],
  toggleCalendarFilter: vi.fn(),
  setActiveCalendarIds: vi.fn(),
  addCalendar: vi.fn(),
  deleteCalendar,
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { useUiStore } from '@/stores/ui-store';
import { CalendarList } from './calendar-list';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  calendarState.calendars = [...initialCalendars];
});

function ownedCalendar(): Calendar {
  const calendar = calendarState.calendars.find((item) => item.id === 'cal-owned');
  if (!calendar) throw new Error('Owned calendar fixture missing');
  return calendar;
}

describe('CalendarList cover images', () => {
  it('shows the uploaded cover thumbnail', () => {
    calendarState.calendars[0] = {
      ...ownedCalendar(),
      coverImageUrl: 'https://storage.example/signed-cover',
    };

    const { container } = render(<CalendarList />);

    expect(container.querySelector('img')).toHaveAttribute(
      'src',
      'https://storage.example/signed-cover',
    );
  });

  it('falls back to the color dot when an uploaded cover fails to load', () => {
    calendarState.calendars[0] = {
      ...ownedCalendar(),
      coverImageUrl: 'https://storage.example/signed-cover',
    };

    const { container } = render(<CalendarList />);
    const uploadedImage = container.querySelector('img');
    expect(uploadedImage).toHaveAttribute('src', 'https://storage.example/signed-cover');
    if (!uploadedImage) throw new Error('Uploaded image was not rendered');

    fireEvent.error(uploadedImage);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('[data-calendar-cover-fallback]')).not.toBeNull();
  });
});

describe('CalendarList share state', () => {
  it('tells a shared calendar from one only the viewer sees', () => {
    calendarState.calendars = [
      { ...(initialCalendars[0] as Calendar), memberCount: 1 },
      { ...(initialCalendars[1] as Calendar), memberCount: 3 },
    ];
    render(<CalendarList />);

    const family = screen.getByRole('button', { name: /Family/ });
    const work = screen.getByRole('button', { name: /Work/ });
    expect(family.textContent).toContain('calendar.onlyMe');
    expect(family.textContent).not.toContain('calendar.sharedWith');
    expect(work.textContent).toContain('calendar.sharedWith');
    expect(work.textContent).not.toContain('calendar.onlyMe');
  });
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

describe('CalendarList after creating a calendar', () => {
  // A new calendar is shared only once someone is invited, so the next step
  // is offered in place and lands on the calendar just made, not the first one.
  it('offers an invite that opens the share panel on the new calendar', async () => {
    const created = {
      id: 'cal-new',
      name: 'Band',
      color: '#47B2F7',
      createdAt: '',
      role: 'owner',
      publicShared: false,
      memberCount: 1,
      memberColor: '#47B2F7',
    };
    calendarState.addCalendar.mockImplementation(async () => {
      calendarState.calendars.push(created);
      return created;
    });
    const user = userEvent.setup();
    render(<CalendarList />);

    await user.click(screen.getByRole('button', { name: 'calendar.newCalendar' }));
    await user.type(screen.getByPlaceholderText('calendar.calendarName'), 'Band{Enter}');
    await user.click(await screen.findByRole('button', { name: 'calendar.inviteMembers' }));

    const ui = useUiStore.getState();
    expect(ui.rightPanel).toBe('share');
    expect(ui.panelCalendarRequest).toEqual({ calendarId: 'cal-new' });
    expect(screen.queryByText('calendar.createdInviteHint')).toBeNull();

    calendarState.calendars.pop();
    useUiStore.getState().resetSessionUi();
  });
});
