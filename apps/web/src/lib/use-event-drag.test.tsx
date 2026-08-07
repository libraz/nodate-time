import { act, renderHook } from '@testing-library/react';
import type { PointerEvent as ReactPointerEvent } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { useEventDrag } from './use-event-drag';

const event = { id: 'e1', title: 'Event' } as CalendarEvent;

let frames: FrameRequestCallback[] = [];

beforeEach(() => {
  frames = [];
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => frames.push(cb));
  vi.stubGlobal('cancelAnimationFrame', (id: number) => {
    frames[id - 1] = () => {};
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function flushFrames() {
  const queued = frames;
  frames = [];
  for (const cb of queued) cb(0);
}

function grab(x: number, y: number): ReactPointerEvent {
  return { pointerType: 'mouse', button: 0, clientX: x, clientY: y } as ReactPointerEvent;
}

/** jsdom has no PointerEvent; the handlers only read the mouse coordinates. */
function pointer(type: string, x: number, y: number): Event {
  return new MouseEvent(type, { clientX: x, clientY: y });
}

// A pointer reports far more often than the screen repaints, and the whole
// month view re-renders behind this state.
describe('useEventDrag pointer sampling', () => {
  it('publishes one drag state per frame, not per sample', async () => {
    const { result } = renderHook(() => useEventDrag({ onDrop: vi.fn() }));

    act(() => {
      result.current.start(event, grab(0, 0));
    });
    act(() => {
      window.dispatchEvent(pointer('pointermove', 40, 0));
      window.dispatchEvent(pointer('pointermove', 60, 0));
      window.dispatchEvent(pointer('pointermove', 80, 0));
    });

    expect(result.current.drag).toBeNull();
    expect(frames).toHaveLength(1);

    act(() => {
      flushFrames();
    });

    // The frame draws where the pointer is now, not where it entered.
    expect(result.current.drag?.x).toBe(80);
  });

  it('drops on release even when the samples have not been drawn yet', async () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useEventDrag({ onDrop }));

    act(() => {
      result.current.start(event, grab(0, 0));
    });
    act(() => {
      window.dispatchEvent(pointer('pointermove', 40, 0));
      window.dispatchEvent(pointer('pointerup', 42, 0));
    });

    expect(onDrop).toHaveBeenCalledTimes(1);
    expect(result.current.consumeClick()).toBe(true);
  });

  it('does not put the ghost back after the drop', async () => {
    const { result } = renderHook(() => useEventDrag({ onDrop: vi.fn() }));

    act(() => {
      result.current.start(event, grab(0, 0));
    });
    act(() => {
      window.dispatchEvent(pointer('pointermove', 40, 0));
    });
    act(() => {
      window.dispatchEvent(pointer('pointerup', 42, 0));
      flushFrames();
    });

    expect(result.current.drag).toBeNull();
  });

  it('ignores movement below the threshold', async () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useEventDrag({ onDrop }));

    act(() => {
      result.current.start(event, grab(0, 0));
    });
    act(() => {
      window.dispatchEvent(pointer('pointermove', 2, 0));
      flushFrames();
    });

    expect(result.current.drag).toBeNull();
    expect(frames).toHaveLength(0);

    act(() => {
      window.dispatchEvent(pointer('pointerup', 2, 0));
    });
    expect(onDrop).not.toHaveBeenCalled();
  });
});
