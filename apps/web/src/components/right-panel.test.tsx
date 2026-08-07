import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const uiState = {
  showSettings: true,
  toggleSettings: vi.fn(),
  theme: 'glass',
  colorMode: 'light',
  locale: 'ja',
  setTheme: vi.fn(),
  setColorMode: vi.fn(),
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  memos: [],
  toggleMemo: vi.fn(),
  deleteMemo: vi.fn(),
  calendars: [],
  activeCalendarIds: [],
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

const authState = { saveAccountPreference: vi.fn().mockResolvedValue(undefined) };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { SettingsModal } from './right-panel';

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

beforeEach(() => {
  uiState.showSettings = true;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  document.body.style.overflow = '';
});

// The dialog was reachable only with a pointer: no scroll lock, no Escape, and
// Tab carried on into the calendar it was covering.
describe('SettingsModal keyboard', () => {
  it('locks the page behind it and closes on Escape', () => {
    render(<SettingsModal />);

    expect(document.body.style.overflow).toBe('hidden');

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(uiState.toggleSettings).toHaveBeenCalledTimes(1);
  });

  it('keeps Tab inside the dialog', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    render(<SettingsModal />);

    const dialog = await screen.findByRole('dialog');
    const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button'));
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('returns focus to whatever opened it', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    uiState.showSettings = false;
    function Harness() {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          <SettingsModal />
        </>
      );
    }
    const { rerender } = render(<Harness />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    uiState.showSettings = true;
    rerender(<Harness />);
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    uiState.showSettings = false;
    rerender(<Harness />);

    expect(document.activeElement).toBe(trigger);
  });
});
