import type { TranslationKey } from '@/i18n';

/**
 * A share/join link as the API renders it.
 *
 * `token` is present only on the response that created the invite. The server
 * stores a hash, not the link, so listing invites cannot hand a working one
 * back — a database read must not yield a usable capability. Anything showing
 * a link therefore has to treat it as shown-once and say so, rather than
 * assuming it can render one for every row.
 */
export interface InviteData {
  /** Public UUID, not the internal row id. */
  id: string;
  token?: string;
  role: string;
  maxUses: number | null;
  useCount: number;
  isPublic: boolean;
  expiresAt: string | null;
  createdAt: string;
}

/** A public/embed link is a non-consuming, read-only viewer link. */
export function isPublicLink(invite: InviteData): boolean {
  return invite.isPublic;
}

/**
 * Merges freshly listed invites with the tokens already held from this
 * session's creations, so re-opening a panel does not blank a link the user
 * has not copied yet. Revoked invites drop out with the list.
 */
export function mergeInviteTokens(listed: InviteData[], known: InviteData[]): InviteData[] {
  const tokens = new Map<string, string>();
  for (const i of known) {
    if (i.token) tokens.set(i.id, i.token);
  }
  return listed.map((i) => {
    if (i.token) return i;
    const token = tokens.get(i.id);
    return token ? { ...i, token } : i;
  });
}

/**
 * How long a join link stays usable, and how many people it admits.
 *
 * A link with neither bound is a standing invitation: it works for whoever
 * finds it, forever, and the only remedy left is revoking it after the fact.
 * Offering both makes the narrow choice the easy one.
 */
export const INVITE_EXPIRY_HOURS = [24, 168, 720, 0] as const;
export const INVITE_MAX_USES = [1, 5, 0] as const;

/** i18n key for each expiry choice. 0 means no expiry. */
export function inviteExpiryLabelKey(hours: number): TranslationKey {
  switch (hours) {
    case 24:
      return 'invites.expiry24h';
    case 168:
      return 'invites.expiry7d';
    case 720:
      return 'invites.expiry30d';
    default:
      return 'invites.expiryNever';
  }
}

/** i18n key for each use-count choice. 0 means unlimited. */
export function inviteUsesLabelKey(uses: number): TranslationKey {
  switch (uses) {
    case 1:
      return 'invites.usesOnce';
    case 5:
      return 'invites.usesFive';
    default:
      return 'invites.usesUnlimited';
  }
}

/**
 * Builds the create-invite body. A zero bound is omitted rather than sent:
 * the API reads an absent field as "no limit", and sending zero would ask for
 * a link nobody can use.
 */
export function inviteCreateBody(role: string, expiryHours: number, maxUses: number) {
  return {
    role,
    ...(expiryHours > 0 ? { expiresInHours: expiryHours } : {}),
    ...(maxUses > 0 ? { maxUses } : {}),
  };
}
