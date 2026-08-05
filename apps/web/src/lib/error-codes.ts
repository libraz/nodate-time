import type { TranslationKey } from '@/i18n';
import { ja } from '@/i18n/ja';

/**
 * The i18n key for an API error code, or null when there is none.
 *
 * Every code the server can return is meant to have an entry, and a Go test
 * walks the error definitions and fails when one is missing. The lookup is
 * still guarded: an unknown key renders as the key itself, so a code added on
 * the server before its message lands here would put `apiError.SOMETHING` in
 * front of a user -- worse than the English sentence the server sent.
 */
export function errorMessageKey(code: string): TranslationKey | null {
  if (!code) return null;
  const key = `apiError.${code}`;
  return key in ja ? (key as TranslationKey) : null;
}

/**
 * Codes that mean the session is over rather than the request was wrong.
 *
 * Kept apart from the message mapping because these change what the client
 * does, not just what it says.
 */
export const SESSION_ENDED_CODES = new Set(['AUTH.TOKEN_INVALID', 'AUTH.TOKEN_MISSING']);
