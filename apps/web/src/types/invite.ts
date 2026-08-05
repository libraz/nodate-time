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
