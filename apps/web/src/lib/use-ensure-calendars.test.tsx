import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  calendars: [] as unknown[],
  isLoading: false,
  fetchCalendars: vi.fn(),
}));

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof state) => unknown) => selector(state),
}));

import { useEnsureCalendars } from './use-ensure-calendars';

function Probe({ enabled = true }: { enabled?: boolean }) {
  useEnsureCalendars(enabled);
  return null;
}

beforeEach(() => {
  state.calendars = [];
  state.isLoading = false;
  state.fetchCalendars.mockReset().mockResolvedValue(undefined);
});

afterEach(cleanup);

describe('useEnsureCalendars', () => {
  it('loads the list when nothing has loaded it', () => {
    render(<Probe />);
    expect(state.fetchCalendars).toHaveBeenCalledTimes(1);
  });

  it('does not load again on re-render after a load that came back empty', () => {
    const { rerender } = render(<Probe />);
    state.isLoading = true;
    rerender(<Probe />);
    state.isLoading = false;
    rerender(<Probe />);
    expect(state.fetchCalendars).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['the list is loaded', () => (state.calendars = [{ id: 'c' }])],
    ['a load is already running', () => (state.isLoading = true)],
  ])('stays quiet when %s', (_, arrange) => {
    arrange();
    render(<Probe />);
    expect(state.fetchCalendars).not.toHaveBeenCalled();
  });

  it('stays quiet while disabled', () => {
    render(<Probe enabled={false} />);
    expect(state.fetchCalendars).not.toHaveBeenCalled();
  });
});
