import { DateTime } from 'luxon';
import { beforeEach, describe, expect, it } from 'vitest';
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
});
