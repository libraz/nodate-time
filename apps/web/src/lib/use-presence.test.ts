import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { EXIT_MS, useLatch, usePresence } from './use-presence';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function stubReducedMotion(reduce: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockReturnValue({ matches: reduce }) as unknown as typeof window.matchMedia,
  );
  window.matchMedia = globalThis.matchMedia;
}

describe('usePresence', () => {
  it('stays mounted and marked closing until the exit has played', () => {
    stubReducedMotion(false);
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ open }) => usePresence(open), {
      initialProps: { open: true },
    });
    expect(result.current).toEqual({ mounted: true, closing: false });

    rerender({ open: false });
    expect(result.current).toEqual({ mounted: true, closing: true });

    act(() => vi.advanceTimersByTime(EXIT_MS));
    expect(result.current).toEqual({ mounted: false, closing: false });
  });

  it('reopening during the exit keeps it mounted and open', () => {
    stubReducedMotion(false);
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ open }) => usePresence(open), {
      initialProps: { open: true },
    });
    rerender({ open: false });
    rerender({ open: true });
    act(() => vi.advanceTimersByTime(EXIT_MS));
    expect(result.current).toEqual({ mounted: true, closing: false });
  });

  it('unmounts at once when reduced motion is asked for', () => {
    stubReducedMotion(true);
    const { result, rerender } = renderHook(({ open }) => usePresence(open), {
      initialProps: { open: true },
    });
    rerender({ open: false });
    expect(result.current.mounted).toBe(false);
  });
});

describe('useLatch', () => {
  it('stays true once the value has been true', () => {
    const { result, rerender } = renderHook(({ v }) => useLatch(v), { initialProps: { v: false } });
    expect(result.current).toBe(false);
    rerender({ v: true });
    rerender({ v: false });
    expect(result.current).toBe(true);
  });
});
