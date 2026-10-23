const REDIRECT_MAX_RUNES = 512;

/**
 * Reports whether a post-login return path is safe to navigate to. Mirrors the
 * API's rule: a same-origin path with a single leading slash, no backslash,
 * no CR/LF, and at most 512 characters.
 */
export function isSafeRedirect(raw: string | undefined | null): raw is string {
  if (!raw) return false;
  if (!raw.startsWith('/') || raw.startsWith('//')) return false;
  if (/[\r\n\\]/.test(raw)) return false;
  return [...raw].length <= REDIRECT_MAX_RUNES;
}

/** The return path to navigate to, or `/` when the value is absent or unsafe. */
export function safeRedirect(raw: string | undefined | null): string {
  return isSafeRedirect(raw) ? raw : '/';
}
