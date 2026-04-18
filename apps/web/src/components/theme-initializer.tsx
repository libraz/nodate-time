import { applyColorMode, applyTheme, watchSystemColorScheme } from '@/lib/theme';
import { useUiStore } from '@/stores/ui-store';
import { useEffect } from 'react';

export function ThemeInitializer() {
  const theme = useUiStore((s) => s.theme);
  const colorMode = useUiStore((s) => s.colorMode);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    applyColorMode(colorMode);

    if (colorMode === 'system') {
      return watchSystemColorScheme((isDark) => {
        document.documentElement.setAttribute('data-mode', isDark ? 'dark' : 'light');
      });
    }
  }, [colorMode]);

  return null;
}
