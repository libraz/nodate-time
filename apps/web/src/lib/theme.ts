import type { TranslationKey } from '@/i18n';

export type ThemeStyle = 'glass' | 'classic' | 'nothing' | 'modern' | 'washi';
export type ColorMode = 'light' | 'dark' | 'system';

/** Selectable themes with their i18n label keys, in display order. */
export const THEME_OPTIONS: { value: ThemeStyle; labelKey: TranslationKey }[] = [
  { value: 'glass', labelKey: 'settings.themeGlass' },
  { value: 'modern', labelKey: 'settings.themeModern' },
  { value: 'washi', labelKey: 'settings.themeWashi' },
  { value: 'classic', labelKey: 'settings.themeClassic' },
  { value: 'nothing', labelKey: 'settings.themeNothing' },
];

export function applyTheme(theme: ThemeStyle): void {
  // 'glass' is the default (no data-theme attribute), others set explicitly
  if (theme === 'glass') {
    document.documentElement.removeAttribute('data-theme');
  } else {
    document.documentElement.setAttribute('data-theme', theme);
  }
}

export function applyColorMode(mode: ColorMode): void {
  if (mode === 'system') {
    const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    document.documentElement.setAttribute('data-mode', isDark ? 'dark' : 'light');
  } else {
    document.documentElement.setAttribute('data-mode', mode);
  }
}

export function watchSystemColorScheme(callback: (isDark: boolean) => void): () => void {
  const mql = window.matchMedia('(prefers-color-scheme: dark)');
  const handler = (e: MediaQueryListEvent) => callback(e.matches);
  mql.addEventListener('change', handler);
  return () => mql.removeEventListener('change', handler);
}
