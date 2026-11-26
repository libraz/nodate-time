import { DateTime } from 'luxon';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { detectHolidayCountry } from '@/lib/holidays';
import { useUiStore } from './ui-store';

beforeEach(() => {
  localStorage.clear();
  useUiStore.setState({
    showEventModal: false,
    editingEventId: null,
    showDayDetail: false,
    rightPanel: null,
    showSearch: false,
    searchQuery: '',
    currentMonth: DateTime.local(2026, 4, 1),
    scrollToTodaySignal: 0,
  });
});

describe('event modal', () => {
  it('opens for a new event with no editing id', () => {
    useUiStore.getState().openEventModal();
    const s = useUiStore.getState();
    expect(s.showEventModal).toBe(true);
    expect(s.editingEventId).toBeNull();
  });

  it('opens for editing with the given id and resets on close', () => {
    useUiStore.getState().openEventModal('evt-1');
    expect(useUiStore.getState().editingEventId).toBe('evt-1');

    useUiStore.getState().closeEventModal();
    const s = useUiStore.getState();
    expect(s.showEventModal).toBe(false);
    expect(s.editingEventId).toBeNull();
  });
});

// The desktop editor stays open beside the calendar, so a second open is a
// request the editor answers (apply, or ask first), never an instant swap.
describe('event modal opened again while open', () => {
  const day = DateTime.local(2026, 4, 20);

  beforeEach(() => {
    useUiStore.setState({
      eventOpenSeq: 0,
      eventDraftDay: null,
      pendingEventOpen: null,
      eventDraftStart: null,
      eventDraftEnd: null,
      selectedDate: day,
    });
  });

  it('opens a closed editor at once and counts the opening', () => {
    useUiStore.getState().openEventModal();
    const s = useUiStore.getState();
    expect(s.showEventModal).toBe(true);
    expect(s.eventOpenSeq).toBe(1);
    expect(s.eventDraftDay?.toISODate()).toBe('2026-04-20');
    expect(s.pendingEventOpen).toBeNull();
  });

  it('leaves the draft day empty when a start was given', () => {
    useUiStore.getState().openEventModal(undefined, day.set({ hour: 10 }));
    expect(useUiStore.getState().eventDraftDay).toBeNull();
  });

  it('queues an open made while the editor is open, without touching what it shows', () => {
    useUiStore.getState().openEventModal('evt-1');
    useUiStore.getState().setSelectedDate(DateTime.local(2026, 4, 22));
    useUiStore.getState().openEventModal('evt-2');

    const s = useUiStore.getState();
    expect(s.editingEventId).toBe('evt-1');
    expect(s.eventOpenSeq).toBe(1);
    expect(s.pendingEventOpen).toMatchObject({
      eventId: 'evt-2',
      draftStart: null,
      draftEnd: null,
    });
    expect(s.pendingEventOpen?.draftDay.toISODate()).toBe('2026-04-22');
  });

  it('applies a queued open and counts it', () => {
    useUiStore.getState().openEventModal('evt-1');
    useUiStore.getState().openEventModal();
    useUiStore.getState().applyPendingEventOpen();

    const s = useUiStore.getState();
    expect(s.editingEventId).toBeNull();
    expect(s.eventOpenSeq).toBe(2);
    expect(s.eventDraftDay?.toISODate()).toBe('2026-04-20');
    expect(s.pendingEventOpen).toBeNull();
  });

  it('applies a queued draft start and drops the draft day', () => {
    const start = DateTime.local(2026, 4, 21, 9);
    const end = DateTime.local(2026, 4, 21, 11);
    useUiStore.getState().openEventModal('evt-1');
    useUiStore.getState().openEventModal(undefined, start, end);
    useUiStore.getState().applyPendingEventOpen();

    const s = useUiStore.getState();
    expect(s.eventDraftStart?.toISO()).toBe(start.toISO());
    expect(s.eventDraftEnd?.toISO()).toBe(end.toISO());
    expect(s.eventDraftDay).toBeNull();
  });

  it('drops a queued open on clear, leaving the editor as it was', () => {
    useUiStore.getState().openEventModal('evt-1');
    useUiStore.getState().openEventModal('evt-2');
    useUiStore.getState().clearPendingEventOpen();

    const s = useUiStore.getState();
    expect(s.pendingEventOpen).toBeNull();
    expect(s.editingEventId).toBe('evt-1');
    expect(s.eventOpenSeq).toBe(1);
  });

  it.each([
    ['closeEventModal', () => useUiStore.getState().closeEventModal()],
    ['resetSessionUi', () => useUiStore.getState().resetSessionUi()],
  ])('forgets the queued open and the draft day on %s', (_, end) => {
    useUiStore.getState().openEventModal();
    useUiStore.getState().openEventModal('evt-2');

    end();

    const s = useUiStore.getState();
    expect(s.pendingEventOpen).toBeNull();
    expect(s.eventDraftDay).toBeNull();
  });
});

describe('day detail', () => {
  it('opens with the selected date and closes', () => {
    const date = DateTime.local(2026, 4, 20);
    useUiStore.getState().openDayDetail(date);
    let s = useUiStore.getState();
    expect(s.showDayDetail).toBe(true);
    expect(s.selectedDate.toISODate()).toBe('2026-04-20');

    useUiStore.getState().closeDayDetail();
    s = useUiStore.getState();
    expect(s.showDayDetail).toBe(false);
  });
});

describe('navigateMonth', () => {
  it('moves the current month forward and backward', () => {
    useUiStore.getState().navigateMonth(1);
    expect(useUiStore.getState().currentMonth.toISODate()).toBe('2026-05-01');
    useUiStore.getState().navigateMonth(-2);
    expect(useUiStore.getState().currentMonth.toISODate()).toBe('2026-03-01');
  });
});

describe('toggleRightPanel', () => {
  it('opens a panel, then closes it when toggled again', () => {
    useUiStore.getState().toggleRightPanel('memo');
    expect(useUiStore.getState().rightPanel).toBe('memo');

    useUiStore.getState().toggleRightPanel('memo');
    expect(useUiStore.getState().rightPanel).toBeNull();
  });

  it('switches directly to a different panel', () => {
    useUiStore.getState().toggleRightPanel('memo');
    useUiStore.getState().toggleRightPanel('members');
    expect(useUiStore.getState().rightPanel).toBe('members');
  });
});

describe('toggleSearch', () => {
  it('flips visibility and clears the query', () => {
    useUiStore.setState({ searchQuery: 'meeting' });
    useUiStore.getState().toggleSearch();
    const s = useUiStore.getState();
    expect(s.showSearch).toBe(true);
    expect(s.searchQuery).toBe('');
  });
});

describe('triggerScrollToToday', () => {
  it('increments the signal each call', () => {
    useUiStore.getState().triggerScrollToToday();
    useUiStore.getState().triggerScrollToToday();
    expect(useUiStore.getState().scrollToTodaySignal).toBe(2);
  });
});

describe('persisted preferences', () => {
  it('writes the theme to localStorage and updates state', () => {
    useUiStore.getState().setTheme('classic');
    expect(useUiStore.getState().theme).toBe('classic');
    expect(localStorage.getItem('tt_theme')).toBe('"classic"');
  });

  it('persists the holidays country selection', () => {
    useUiStore.getState().setHolidaysCountry('US');
    expect(useUiStore.getState().holidaysCountry).toBe('US');
    expect(localStorage.getItem('tt_holidaysCountry')).toBe('"US"');
  });

  it('starts from the country the browser implies, not a fixed one', () => {
    // jsdom reports en-US, so anything but US here means the default was
    // picked for one country's users rather than derived.
    expect(useUiStore.getInitialState().holidaysCountry).toBe(detectHolidayCountry());
    expect(useUiStore.getInitialState().holidaysCountry).toBe('US');
  });
});

describe('weekStart', () => {
  it('starts on Sunday when nothing is stored', () => {
    expect(useUiStore.getInitialState().weekStart).toBe('sun');
  });

  it('adopts and persists an account week start', () => {
    useUiStore.getState().adoptAccountPreferences({ weekStart: 'mon' });
    expect(useUiStore.getState().weekStart).toBe('mon');
    expect(localStorage.getItem('tt_weekStart')).toBe('"mon"');
  });

  it('leaves the week start alone when the preferences carry none', () => {
    useUiStore.setState({ weekStart: 'sat' });
    useUiStore.getState().adoptAccountPreferences({ locale: 'en' });
    expect(useUiStore.getState().weekStart).toBe('sat');
  });

  it('reads a stored week start on load', async () => {
    localStorage.setItem('tt_weekStart', '"sat"');
    vi.resetModules();
    const { useUiStore: fresh } = await import('./ui-store');
    expect(fresh.getState().weekStart).toBe('sat');
  });

  it.each(['"tue"', '"MON"', '1', 'null', '{bad'])('ignores the stored value %s', async (raw) => {
    localStorage.setItem('tt_weekStart', raw);
    vi.resetModules();
    const { useUiStore: fresh } = await import('./ui-store');
    expect(fresh.getState().weekStart).toBe('sun');
  });
});
