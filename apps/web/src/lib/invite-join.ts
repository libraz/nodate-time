import { getT } from '@/i18n';
import { client } from '@/lib/client';
import { isSafeRedirect, safeRedirect } from '@/lib/safe-redirect';
import { toast } from '@/lib/toast';

const SHARE_PATH = /^\/share\/([^/]+)$/;

/**
 * The invite token a sign-in return path asks to join, or null when the path
 * is anything else. Only `/share/{token}?join=1` carries that intent.
 */
export function inviteTokenToJoin(redirect: string | undefined | null): string | null {
  if (!isSafeRedirect(redirect)) return null;
  const url = new URL(redirect, 'http://localhost');
  if (url.searchParams.get('join') !== '1') return null;
  const encoded = url.pathname.match(SHARE_PATH)?.[1];
  if (!encoded) return null;
  try {
    return decodeURIComponent(encoded);
  } catch {
    return null;
  }
}

/** The name of the calendar just joined; a failed lookup only costs the toast its name. */
async function calendarName(calendarId: string): Promise<string> {
  try {
    const { data } = await client.GET('/calendars/{calendarId}', {
      params: { path: { calendarId } },
    });
    return data?.name ?? '';
  } catch {
    return '';
  }
}

/**
 * Where to go once the user is signed in. A return path that asks to join an
 * invite accepts it first and reports the outcome in a toast: on success the
 * app opens, on failure the share page stays up so the reason can be read and
 * the join retried. Any other path is returned as given, if it is safe.
 */
export async function resolveSignInTarget(redirect: string | undefined | null): Promise<string> {
  const token = inviteTokenToJoin(redirect);
  if (!token) return safeRedirect(redirect);
  const t = getT();
  try {
    const { data } = await client.POST('/invites/{token}/accept', {
      params: { path: { token } },
    });
    const name = data ? await calendarName(data.calendarId) : '';
    toast.success(t('invite.joined', { name }));
    return '/';
  } catch {
    toast.error(t('invite.joinFailed'));
    return `/share/${encodeURIComponent(token)}`;
  }
}
