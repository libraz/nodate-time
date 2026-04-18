import { useUiStore } from '@/stores/ui-store';
import { en } from './en';
import { ja } from './ja';
import type { TranslationKey } from './ja';

const translations = { ja, en } as const;

export type { TranslationKey };
export type Locale = 'ja' | 'en';

type TranslateFn = (key: TranslationKey, params?: Record<string, string | number>) => string;

function createTranslateFn(locale: Locale): TranslateFn {
  const dict = translations[locale];
  return (key, params) => {
    let text: string = dict[key] ?? key;
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        text = text.replace(`{${k}}`, String(v));
      }
    }
    return text;
  };
}

/** React hook — re-renders when locale changes */
export function useT(): TranslateFn {
  const locale = useUiStore((s) => s.locale);
  return createTranslateFn(locale);
}

/** Non-reactive getter for use outside React (e.g. Zustand stores) */
export function getT(): TranslateFn {
  const locale = useUiStore.getState().locale;
  return createTranslateFn(locale);
}

export const MONTH_NAMES_EN = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
] as const;
