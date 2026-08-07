import { useCallback, useEffect, useState } from 'react';
import { useT } from '@/i18n';
import { api, errorMessage } from '@/lib/api';
import type { Role } from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { type InviteData, inviteCreateBody, isPublicLink, mergeInviteTokens } from '@/types/invite';

/** What a join link is created with: the grant it carries, and the bounds on it. */
export interface InviteTerms {
  role: Role;
  /** Hours the link stays usable; 0 means no expiry. */
  expiryHours: number;
  /** How many people it admits; 0 means unlimited. */
  maxUses: number;
}

/**
 * The role a public embed link grants. Reading, and only reading: the link is
 * handed to people who are not members and cannot be taken back from whoever
 * it reaches.
 */
const PUBLIC_LINK_ROLE: Role = 'viewer';

export interface CalendarInvites {
  invites: InviteData[];
  /** Links that can be joined. The public embed link is not one of them. */
  joinInvites: InviteData[];
  /** The calendar's public embed link; there is at most one. */
  publicLink: InviteData | null;
  loading: boolean;
  /** Which creation is in flight, so a screen can disable the one button it owns. */
  busy: 'invite' | 'public' | null;
  /** Each operation reports whether it went through, leaving the wording to the caller. */
  createInvite: (terms: InviteTerms) => Promise<boolean>;
  createPublicLink: () => Promise<boolean>;
  revokeInvite: (id: string) => Promise<boolean>;
}

/**
 * A calendar's share links, and everything that can be done to them.
 *
 * Two screens offer this — the settings tab and the share panel — and holding
 * one implementation is what keeps them from disagreeing about which roles a
 * link may grant or what a failure says.
 *
 * `enabled` gates the listing: only a manager may read a calendar's invites,
 * so asking regardless earned an editor a permission error for opening a
 * screen, once per calendar they selected.
 */
export function useInvites(calendarId: string, enabled = true): CalendarInvites {
  const t = useT();
  const [invites, setInvites] = useState<InviteData[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState<'invite' | 'public' | null>(null);

  useEffect(() => {
    if (!enabled || !calendarId) {
      setInvites([]);
      setLoading(false);
      return;
    }
    // A superseded listing must not land: switching calendars faster than the
    // requests come back otherwise leaves one calendar's links listed under
    // another calendar's name.
    let cancelled = false;
    setLoading(true);
    (async () => {
      try {
        const list = await api.get<InviteData[]>(`/calendars/${calendarId}/invites`);
        // A listing carries no tokens; keep the ones this session created so a
        // freshly made link does not vanish before it has been copied.
        if (!cancelled) setInvites((cur) => mergeInviteTokens(list, cur));
      } catch (e) {
        if (!cancelled) toast.error(errorMessage(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [calendarId, enabled]);

  const createInvite = useCallback(
    async (terms: InviteTerms) => {
      if (!calendarId) return false;
      setBusy('invite');
      try {
        const created = await api.post<InviteData>(
          `/calendars/${calendarId}/invites`,
          inviteCreateBody(terms.role, terms.expiryHours, terms.maxUses),
        );
        setInvites((cur) => [created, ...cur]);
        return true;
      } catch (e) {
        toast.error(errorMessage(e));
        return false;
      } finally {
        setBusy(null);
      }
    },
    [calendarId],
  );

  const createPublicLink = useCallback(async () => {
    if (!calendarId) return false;
    // Guard against accidental external exposure.
    if (!window.confirm(t('share.publicConfirm'))) return false;
    setBusy('public');
    try {
      const created = await api.post<InviteData>(`/calendars/${calendarId}/invites`, {
        role: PUBLIC_LINK_ROLE,
        isPublic: true,
      });
      setInvites((cur) => [created, ...cur.filter((i) => !isPublicLink(i))]);
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      return false;
    } finally {
      setBusy(null);
    }
  }, [calendarId, t]);

  const revokeInvite = useCallback(
    async (id: string) => {
      if (!calendarId) return false;
      try {
        await api.delete(`/calendars/${calendarId}/invites/${id}`);
        setInvites((cur) => cur.filter((i) => i.id !== id));
        return true;
      } catch (e) {
        toast.error(errorMessage(e));
        return false;
      }
    },
    [calendarId],
  );

  return {
    invites,
    joinInvites: invites.filter((i) => !isPublicLink(i)),
    publicLink: invites.find(isPublicLink) ?? null,
    loading,
    busy,
    createInvite,
    createPublicLink,
    revokeInvite,
  };
}
