import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { EMPTY_FILTER } from '@/lib/event-filter';
import { useIsEventShown } from '@/lib/use-shown-events';
import { useCalendarStore } from '@/stores/calendar-store';
import { makeEvent } from '@/test/make-event';

describe('useIsEventShown', () => {
  afterEach(() => {
    useCalendarStore.setState({ activeCalendarIds: [], eventFilter: EMPTY_FILTER });
  });

  it('needs both a shown calendar and a passing filter', () => {
    const ownedByA = makeEvent({ calendarId: 'cal-1', ownerId: 'a' });
    const ownedByB = makeEvent({ calendarId: 'cal-1', ownerId: 'b' });
    const onHidden = makeEvent({ calendarId: 'cal-2', ownerId: 'a' });
    useCalendarStore.setState({ activeCalendarIds: ['cal-1'], eventFilter: EMPTY_FILTER });

    const { result } = renderHook(() => useIsEventShown());
    expect([ownedByA, ownedByB, onHidden].map(result.current)).toEqual([true, true, false]);

    act(() =>
      useCalendarStore.setState({
        eventFilter: {
          match: 'all',
          conditions: [
            { id: 'c', field: 'owner', exclude: false, people: [{ id: 'a', name: 'A' }] },
          ],
        },
      }),
    );
    expect([ownedByA, ownedByB, onHidden].map(result.current)).toEqual([true, false, false]);
  });

  it('keeps the same predicate while nothing it reads changes', () => {
    const { result, rerender } = renderHook(() => useIsEventShown());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});
