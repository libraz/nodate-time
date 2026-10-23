import { act, renderHook } from '@testing-library/react';
import type { PointerEvent as ReactPointerEvent } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { useEventDrag, useEventResize } from './use-event-drag';

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
  vi.useRealTimers();
});

function flushFrames() {
  const queued = frames;
  frames = [];
  for (const cb of queued) cb(0);
}

function press(pointerType: string, x: number, y: number): ReactPointerEvent {
  return {
    pointerType,
    button: 0,
    clientX: x,
    clientY: y,
    stopPropagation: () => {},
  } as ReactPointerEvent;
}

/** jsdom has no PointerEvent; the handlers only read the pointer coordinates. */
function pointer(type: string, x: number, y: number): Event {
  return new MouseEvent(type, { clientX: x, clientY: y });
}

describe('pointer cancellation', () => {
  it('cancels a move without dropping or restoring its ghost', () => {
    const onDrop = vi.fn();
    const { result } = renderHook(() => useEventDrag({ onDrop }));

    act(() => {
      result.current.start(event, press('mouse', 0, 0));
      window.dispatchEvent(pointer('pointermove', 40, 0));
      window.dispatchEvent(pointer('pointercancel', 42, 0));
    });

    expect(onDrop).not.toHaveBeenCalled();
    act(() => {
      flushFrames();
      window.dispatchEvent(pointer('pointerup', 44, 0));
    });

    expect(result.current.drag).toBeNull();
    expect(onDrop).not.toHaveBeenCalled();
    expect(result.current.consumeClick()).toBe(false);
  });

  it('clears a touch long-press timer when canceled before activation', () => {
    vi.useFakeTimers();
    const onDrop = vi.fn();
    const { result } = renderHook(() => useEventDrag({ onDrop, longPressMs: 350 }));

    act(() => {
      result.current.start(event, press('touch', 0, 0));
      window.dispatchEvent(pointer('pointercancel', 0, 0));
      vi.advanceTimersByTime(400);
    });

    expect(result.current.drag).toBeNull();
    expect(onDrop).not.toHaveBeenCalled();
    expect(result.current.consumeClick()).toBe(false);
  });

  it('ends a canceled resize once without committing or suppressing a click', () => {
    const onPreview = vi.fn();
    const onCommit = vi.fn();
    const onEnd = vi.fn();
    const { result } = renderHook(() => useEventResize<string>({ onPreview, onCommit, onEnd }));

    act(() => {
      result.current.start(event, press('touch', 0, 100), 'seg');
      window.dispatchEvent(pointer('pointermove', 0, 140));
      window.dispatchEvent(pointer('pointercancel', 0, 148));
      window.dispatchEvent(pointer('pointerup', 0, 152));
    });

    expect(onPreview).toHaveBeenCalledWith(event, 140, 'seg');
    expect(onEnd).toHaveBeenCalledTimes(1);
    expect(onCommit).not.toHaveBeenCalled();
    expect(result.current.consumeClick()).toBe(false);
  });
});
