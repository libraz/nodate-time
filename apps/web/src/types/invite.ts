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
 * Every choice here is one the API will honour as given. A join link cannot be
 * asked to live forever: the API takes an expiry of an hour to a year and
 * applies a default of its own -- a week -- to a request that states none, so
 * there is nothing the client can send that means "never". The option that
 * said otherwise produced a link that stopped working after seven days while
 * its label promised it never would.
 *
 * A public link is a different thing and is not created from this list: it
 * carries no expiry at all, because it grants no membership and an embedded
 * calendar going dark every week would be the worse failure.
 */
export const INVITE_EXPIRY_HOURS = [24, 168, 720] as const;
export const INVITE_MAX_USES = [1, 5, 0] as const;

/** One of the expiry choices, in hours. */
export type InviteExpiryHours = (typeof INVITE_EXPIRY_HOURS)[number];

/**
 * i18n key for each expiry choice.
 *
 * Taking the choices themselves as its argument keeps this answerable: a value
 * with no honest label -- a zero standing for "never" among them -- cannot be
 * added to the list without the compiler asking what it should say.
 */
export function inviteExpiryLabelKey(hours: InviteExpiryHours): TranslationKey {
  switch (hours) {
    case 24:
      return 'invites.expiry24h';
    case 168:
      return 'invites.expiry7d';
    default:
      return 'invites.expiry30d';
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
 * Builds the create-invite body.
 *
 * The expiry is always stated. An absent one is not a request for an unlimited
 * link -- the API answers it with a default of its own -- so omitting the
 * field would quietly replace the choice that was made. A zero use limit is
 * still dropped: there an absent field really does mean unlimited, and zero
 * would ask for a link nobody may use.
 */
export function inviteCreateBody(role: string, expiryHours: number, maxUses: number) {
  return {
    role,
    expiresInHours: expiryHours,
    ...(maxUses > 0 ? { maxUses } : {}),
  };
}
