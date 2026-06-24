import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyColorMode, applyTheme, watchSystemColorScheme } from './theme';

/** Install a controllable window.matchMedia stub and return helpers to drive it. */
function stubMatchMedia(initialDark: boolean) {
  let dark = initialDark;
  const listeners = new Set<(e: MediaQueryListEvent) => void>();
  const mql = {
    get matches() {
      return dark;
    },
    media: '(prefers-color-scheme: dark)',
    addEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => listeners.add(cb),
    removeEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => listeners.delete(cb),
  };
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => mql),
  );
  return {
    setDark(value: boolean) {
      dark = value;
      for (const cb of listeners) cb({ matches: value } as MediaQueryListEvent);
    },
    listenerCount: () => listeners.size,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-mode');
});

describe('applyTheme', () => {
  it('removes the data-theme attribute for the default glass theme', () => {
    document.documentElement.setAttribute('data-theme', 'classic');
    applyTheme('glass');
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('sets data-theme for non-default themes', () => {
    applyTheme('classic');
    expect(document.documentElement.getAttribute('data-theme')).toBe('classic');
    applyTheme('nothing');
    expect(document.documentElement.getAttribute('data-theme')).toBe('nothing');
  });
});

describe('applyColorMode', () => {
  it('sets the explicit mode directly', () => {
    applyColorMode('dark');
    expect(document.documentElement.getAttribute('data-mode')).toBe('dark');
    applyColorMode('light');
    expect(document.documentElement.getAttribute('data-mode')).toBe('light');
  });

  it('resolves system mode from the OS preference', () => {
    stubMatchMedia(true);
    applyColorMode('system');
    expect(document.documentElement.getAttribute('data-mode')).toBe('dark');

    stubMatchMedia(false);
    applyColorMode('system');
    expect(document.documentElement.getAttribute('data-mode')).toBe('light');
  });
});

describe('watchSystemColorScheme', () => {
  it('invokes the callback on scheme changes and unsubscribes on dispose', () => {
    const media = stubMatchMedia(false);
    const cb = vi.fn();
    const dispose = watchSystemColorScheme(cb);

    expect(media.listenerCount()).toBe(1);
    media.setDark(true);
    expect(cb).toHaveBeenCalledWith(true);

    dispose();
    expect(media.listenerCount()).toBe(0);
  });
});
