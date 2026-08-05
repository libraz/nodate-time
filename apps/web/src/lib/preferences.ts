import type { Locale } from '@/i18n';

/** The locales this application is translated into. */
const SUPPORTED_LOCALES: Locale[] = ['ja', 'en'];

/**
 * The timezone this device believes it is in.
 *
 * Only a starting point: the account's own setting outranks it, because the
 * times on a calendar have to read the same on every device its owner picks
 * up, not follow whichever one is in hand.
 */
export function detectTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

/** The best supported match for this device's language, defaulting to `ja`. */
export function detectLocale(): Locale {
  const tags = typeof navigator === 'undefined' ? [] : (navigator.languages ?? []);
  for (const tag of tags) {
    const base = tag.split('-')[0]?.toLowerCase();
    const match = SUPPORTED_LOCALES.find((l) => l === base);
    if (match) return match;
  }
  return 'ja';
}

/** An account's stored preferences, with anything unusable left out. */
export interface AccountPreferences {
  locale?: Locale;
  timezone?: string;
}

/**
 * Reads the preferences an account carries, ignoring values this build cannot
 * honour. A locale the server knows and the client does not would otherwise
 * leave every label rendering as its own key.
 */
export function accountPreferences(user: {
  locale?: string;
  timezone?: string;
}): AccountPreferences {
  const prefs: AccountPreferences = {};
  const locale = SUPPORTED_LOCALES.find((l) => l === user.locale);
  if (locale) prefs.locale = locale;
  if (user.timezone && isKnownTimezone(user.timezone)) prefs.timezone = user.timezone;
  return prefs;
}

/**
 * Whether this runtime can resolve a timezone name. A name it cannot resolve
 * would make every date format throw, which is worse than showing the wrong
 * zone.
 */
function isKnownTimezone(tz: string): boolean {
  try {
    Intl.DateTimeFormat(undefined, { timeZone: tz });
    return true;
  } catch {
    return false;
  }
}
