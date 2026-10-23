import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react';
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

interface InviteScope {
  calendarId: string;
  enabled: boolean;
}

interface PendingCreate {
  kind: 'invite' | 'public';
}

type InviteMutation =
  | { kind: 'join'; invite: InviteData }
  | { kind: 'public'; invite: InviteData }
  | { kind: 'revoke'; id: string };

interface InviteActivation {
  scope: InviteScope;
  loading: boolean;
  listingPending: boolean;
  pendingCreates: PendingCreate[];
  journal: InviteMutation[];
}

function applyInviteMutation(rows: InviteData[], mutation: InviteMutation): InviteData[] {
  switch (mutation.kind) {
    case 'join':
      return [mutation.invite, ...rows.filter((row) => row.id !== mutation.invite.id)];
    case 'public':
      return [
        mutation.invite,
        ...rows.filter((row) => row.id !== mutation.invite.id && !isPublicLink(row)),
      ];
    case 'revoke':
      return rows.filter((row) => row.id !== mutation.id);
  }
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
  const scope = useMemo<InviteScope>(() => ({ calendarId, enabled }), [calendarId, enabled]);
  const activationRef = useRef<InviteActivation | null>(null);
  const [invitesByCalendar, setInvitesByCalendar] = useState<Map<string, InviteData[]>>(
    () => new Map(),
  );
  const [, setRevision] = useState(0);
  const refresh = useCallback(() => setRevision((revision) => revision + 1), []);

  // The activation belongs to the committed calendar/enabled pair. The scope
  // identity distinguishes A -> B -> A, while the layout cleanup closes the
  // old scope before callbacks from the new screen can run.
  useLayoutEffect(() => {
    if (!scope.enabled || !scope.calendarId) return;

    const activation: InviteActivation = {
      scope,
      loading: true,
      listingPending: true,
      pendingCreates: [],
      journal: [],
    };
    activationRef.current = activation;
    refresh();

    void api
      .get<InviteData[]>(`/calendars/${scope.calendarId}/invites`)
      .then((listed) => {
        if (activationRef.current !== activation) return;

        // Preserve the created token from current rows, then apply mutations
        // that succeeded while this first listing was still in flight.
        const journal = [...activation.journal];
        activation.journal = [];
        activation.listingPending = false;
        activation.loading = false;
        setInvitesByCalendar((current) => {
          let rows = mergeInviteTokens(listed, current.get(scope.calendarId) ?? []);
          for (const mutation of journal) rows = applyInviteMutation(rows, mutation);
          const next = new Map(current);
          next.set(scope.calendarId, rows);
          return next;
        });
        refresh();
      })
      .catch((error: unknown) => {
        if (activationRef.current !== activation) return;

        activation.journal = [];
        activation.listingPending = false;
        activation.loading = false;
        toast.error(errorMessage(error));
        refresh();
      });

    return () => {
      activation.journal = [];
      activation.listingPending = false;
      activation.pendingCreates = [];
      if (activationRef.current === activation) activationRef.current = null;
    };
  }, [scope, refresh]);

  const commitMutation = useCallback(
    (activation: InviteActivation, mutation: InviteMutation) => {
      if (activationRef.current !== activation) return;

      if (activation.listingPending) activation.journal.push(mutation);
      setInvitesByCalendar((current) => {
        const rows = applyInviteMutation(current.get(activation.scope.calendarId) ?? [], mutation);
        const next = new Map(current);
        next.set(activation.scope.calendarId, rows);
        return next;
      });
      refresh();
    },
    [refresh],
  );

  const startCreate = useCallback(
    (activation: InviteActivation, kind: PendingCreate['kind']) => {
      const operation: PendingCreate = { kind };
      activation.pendingCreates.push(operation);
      refresh();
      return operation;
    },
    [refresh],
  );

  const finishCreate = useCallback(
    (activation: InviteActivation, operation: PendingCreate) => {
      if (activationRef.current !== activation) return;
      activation.pendingCreates = activation.pendingCreates.filter(
        (pending) => pending !== operation,
      );
      refresh();
    },
    [refresh],
  );

  const createInvite = useCallback(
    async (terms: InviteTerms) => {
      const activation = activationRef.current;
      if (!scope.enabled || !scope.calendarId || activation?.scope !== scope) return false;

      const operation = startCreate(activation, 'invite');
      try {
        if (activationRef.current !== activation) return false;
        const created = await api.post<InviteData>(
          `/calendars/${activation.scope.calendarId}/invites`,
          inviteCreateBody(terms.role, terms.expiryHours, terms.maxUses),
        );
        commitMutation(activation, { kind: 'join', invite: created });
        return true;
      } catch (error) {
        if (activationRef.current === activation) toast.error(errorMessage(error));
        return false;
      } finally {
        finishCreate(activation, operation);
      }
    },
    [commitMutation, finishCreate, scope, startCreate],
  );

  const createPublicLink = useCallback(async () => {
    const activation = activationRef.current;
    if (!scope.enabled || !scope.calendarId || activation?.scope !== scope) return false;
    // Guard against accidental external exposure.
    if (!window.confirm(t('share.publicConfirm'))) return false;
    if (activationRef.current !== activation) return false;

    const operation = startCreate(activation, 'public');
    try {
      if (activationRef.current !== activation) return false;
      const created = await api.post<InviteData>(
        `/calendars/${activation.scope.calendarId}/invites`,
        {
          role: PUBLIC_LINK_ROLE,
          isPublic: true,
        },
      );
      commitMutation(activation, { kind: 'public', invite: created });
      return true;
    } catch (error) {
      if (activationRef.current === activation) toast.error(errorMessage(error));
      return false;
    } finally {
      finishCreate(activation, operation);
    }
  }, [commitMutation, finishCreate, scope, startCreate, t]);

  const revokeInvite = useCallback(
    async (id: string) => {
      const activation = activationRef.current;
      if (!scope.enabled || !scope.calendarId || activation?.scope !== scope) return false;

      try {
        if (activationRef.current !== activation) return false;
        await api.delete(`/calendars/${activation.scope.calendarId}/invites/${id}`);
        commitMutation(activation, { kind: 'revoke', id });
        return true;
      } catch (error) {
        if (activationRef.current === activation) toast.error(errorMessage(error));
        return false;
      }
    },
    [commitMutation, scope],
  );

  const activation = activationRef.current?.scope === scope ? activationRef.current : null;
  const invites =
    scope.enabled && scope.calendarId ? (invitesByCalendar.get(scope.calendarId) ?? []) : [];
  const latestCreate = activation?.pendingCreates[activation.pendingCreates.length - 1];

  return {
    invites,
    joinInvites: invites.filter((invite) => !isPublicLink(invite)),
    publicLink: invites.find(isPublicLink) ?? null,
    loading: activation?.loading ?? false,
    busy: latestCreate?.kind ?? null,
    createInvite,
    createPublicLink,
    revokeInvite,
  };
}
