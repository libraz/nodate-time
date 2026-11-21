import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { ConfirmSheet } from '@/components/confirm-sheet';
import { CustomSelect } from '@/components/pickers';
import { SchedulePollCandidateFinder } from '@/components/schedule-poll-candidate-finder';
import {
  AnswerMark,
  AvailabilityBadge,
  formatSchedulePollSlotTime,
  InviteeGrid,
  SchedulePollFrame,
  SchedulePollNotice,
  SchedulePollProgress,
  SchedulePollSection,
  SchedulePollSlot,
  SchedulePollStatus,
} from '@/components/schedule-poll-ui';
import { useT } from '@/i18n';
import { ApiError, errorMessage } from '@/lib/api';
import { memberOption } from '@/lib/member-option';
import {
  type PollAnswer,
  type PollSlot,
  type SchedulePoll,
  type SchedulePollCandidate,
  schedulePollApi,
} from '@/lib/schedule-polls';
import { isStaleError } from '@/lib/stale';
import { toast } from '@/lib/toast';
import { useCalendarMembers } from '@/lib/use-calendar-members';
import { useUnsavedGuard } from '@/lib/use-unsaved-guard';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { useUiStore } from '@/stores/ui-store';
import type { CalendarEvent } from '@/types/calendar';

type PollView = 'respond' | 'manage' | 'candidates' | 'event';

export interface SchedulePollDetailSearch {
  view?: PollView | undefined;
}

export const Route = createFileRoute('/schedule-polls_/$calendarId/$pollId')({
  validateSearch: (search: Record<string, unknown>): SchedulePollDetailSearch => ({
    view:
      search.view === 'respond' ||
      search.view === 'manage' ||
      search.view === 'candidates' ||
      search.view === 'event'
        ? search.view
        : undefined,
  }),
  component: SchedulePollDetailPage,
});

function answerLabel(t: ReturnType<typeof useT>, answer: PollAnswer | undefined) {
  if (answer === 'yes') return `${t('schedulePoll.answerYesMark')} ${t('schedulePoll.answerYes')}`;
  if (answer === 'maybe')
    return `${t('schedulePoll.answerMaybeMark')} ${t('schedulePoll.answerMaybe')}`;
  if (answer === 'no') return `${t('schedulePoll.answerNoMark')} ${t('schedulePoll.answerNo')}`;
  return t('schedulePoll.answerUnanswered');
}

function candidateKey(slot: Pick<PollSlot, 'startAt' | 'endAt'>): string {
  return `${Date.parse(slot.startAt)}/${Date.parse(slot.endAt)}`;
}

// Only recipients of an open poll have an answer to read.
function myResponseOf(calendarId: string, target: SchedulePoll, signal?: AbortSignal) {
  return target.status === 'open' && target.canRespond
    ? schedulePollApi.getResponse(calendarId, target.id, signal)
    : Promise.resolve(null);
}

function SchedulePollDetailPage() {
  const t = useT();
  const navigate = useNavigate();
  const { calendarId, pollId } = Route.useParams();
  const { view: requestedView } = Route.useSearch();
  const userId = useAuthStore((state) => state.user?.id);
  const locale = useUiStore((state) => state.locale);
  const refreshActions = useSchedulePollStore((state) => state.fetchActions);
  const [poll, setPoll] = useState<SchedulePoll | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [refreshWarning, setRefreshWarning] = useState<string | null>(null);
  const [event, setEvent] = useState<CalendarEvent | null>(null);
  const [eventError, setEventError] = useState<'unavailable' | 'missing' | null>(null);
  const [draftAnswers, setDraftAnswers] = useState<Record<string, PollAnswer>>({});
  const [savingAnswers, setSavingAnswers] = useState(false);
  const [stale, setStale] = useState(false);
  const [answerEtag, setAnswerEtag] = useState<string | null>(null);
  const [confirmingPending, setConfirmingPending] = useState(false);
  const [memberIdsDraft, setMemberIdsDraft] = useState<string[]>([]);
  const [savingMembers, setSavingMembers] = useState(false);
  const [keptSlotIds, setKeptSlotIds] = useState<string[]>([]);
  const [newCandidates, setNewCandidates] = useState<SchedulePollCandidate[]>([]);
  const [savingCandidates, setSavingCandidates] = useState(false);
  const [selectedSlotId, setSelectedSlotId] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [acknowledged, setAcknowledged] = useState(false);
  const [finalizing, setFinalizing] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [confirmingCancel, setConfirmingCancel] = useState(false);

  const view: PollView = requestedView ?? (poll?.canManage ? 'manage' : 'respond');
  const loadMyResponse = (target: SchedulePoll, signal?: AbortSignal) =>
    myResponseOf(calendarId, target, signal);
  const reload = async (refresh = false) => {
    const current = await schedulePollApi.get(calendarId, pollId);
    let latest = current;
    setRefreshWarning(null);
    if (refresh && current.status === 'open') {
      try {
        latest = await schedulePollApi.refresh(calendarId, pollId);
      } catch (reason) {
        if (isStaleError(reason)) {
          latest = await schedulePollApi.get(calendarId, pollId);
          setRefreshWarning(t('schedulePoll.staleReloaded'));
        } else {
          setRefreshWarning(errorMessage(reason));
        }
      }
    }
    const mine = await loadMyResponse(latest);
    setPoll(latest);
    setAnswerEtag(mine?.etag ?? null);
    setDraftAnswers(
      Object.fromEntries((mine?.answers ?? latest.myAnswers).map((a) => [a.slotId, a.value])),
    );
    setMemberIdsDraft((latest.members ?? []).map((member) => member.id));
    setKeptSlotIds(latest.slots.filter((slot) => slot.active).map((slot) => slot.id));
    setSelectedSlotId((currentSlot) =>
      latest.slots.some((slot) => slot.id === currentSlot && slot.active)
        ? currentSlot
        : (latest.selectedSlotId ??
          latest.slots.find((slot) => slot.active && slot.availability !== 'hard')?.id ??
          ''),
    );
    setOwnerId((currentOwner) =>
      latest.members?.some((member) => member.id === currentOwner)
        ? currentOwner
        : (latest.members?.find((member) => member.id === userId)?.id ??
          latest.members?.[0]?.id ??
          ''),
    );
    setNewCandidates([]);
    setStale(false);
    setAcknowledged(false);
    return latest;
  };

  useEffect(() => {
    if (!userId) {
      setLoading(true);
      setPoll(null);
      setEvent(null);
      return;
    }
    const controller = new AbortController();
    let active = true;
    setLoading(true);
    setLoadError(null);
    setEvent(null);
    setEventError(null);
    (async () => {
      try {
        let latest = await schedulePollApi.get(calendarId, pollId, controller.signal);
        if (requestedView !== 'event' && latest.status === 'open') {
          try {
            latest = await schedulePollApi.refresh(calendarId, pollId);
          } catch (reason) {
            if (isStaleError(reason))
              latest = await schedulePollApi.get(calendarId, pollId, controller.signal);
            else if (active) setRefreshWarning(errorMessage(reason));
          }
        }
        const mine = await myResponseOf(calendarId, latest, controller.signal);
        if (!active) return;
        setPoll(latest);
        setAnswerEtag(mine?.etag ?? null);
        setDraftAnswers(
          Object.fromEntries((mine?.answers ?? latest.myAnswers).map((a) => [a.slotId, a.value])),
        );
        setMemberIdsDraft((latest.members ?? []).map((member) => member.id));
        setKeptSlotIds(latest.slots.filter((slot) => slot.active).map((slot) => slot.id));
        setSelectedSlotId(
          latest.selectedSlotId ??
            latest.slots.find((slot) => slot.active && slot.availability !== 'hard')?.id ??
            '',
        );
        setOwnerId(
          latest.members?.find((member) => member.id === userId)?.id ??
            latest.members?.[0]?.id ??
            '',
        );
        if (requestedView === 'event' && latest.confirmedEventId) {
          try {
            const loadedEvent = await schedulePollApi.event(
              calendarId,
              latest.confirmedEventId,
              controller.signal,
            );
            if (active) setEvent(loadedEvent);
          } catch (reason) {
            if (!active) return;
            setEventError(
              reason instanceof ApiError && reason.status === 404 ? 'missing' : 'unavailable',
            );
          }
        }
      } catch (reason) {
        if (active && !controller.signal.aborted) setLoadError(errorMessage(reason));
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
      controller.abort();
    };
  }, [calendarId, pollId, requestedView, userId]);

  const rosterCalendarId =
    poll?.canManage && poll.status === 'open' && view !== 'event' ? calendarId : '';
  const { members: calendarMembers } = useCalendarMembers(rosterCalendarId);
  const roster = [...calendarMembers].sort((a, b) => a.name.localeCompare(b.name));

  const navigateView = (next: PollView) => {
    void navigate({
      to: '/schedule-polls/$calendarId/$pollId',
      params: { calendarId, pollId },
      search: { view: next },
    });
  };

  const synchronizeActions = () => {
    if (userId) void refreshActions(userId);
  };

  const saveResponse = async () => {
    if (poll?.status !== 'open' || answerEtag === null) return;
    setSavingAnswers(true);
    setStale(false);
    const savedAnswers = new Map(poll.myAnswers.map((answer) => [answer.slotId, answer.value]));
    const answers = poll.slots
      .filter((slot) => slot.active && slot.availability !== 'hard')
      .flatMap((slot) => {
        const value = draftAnswers[slot.id];
        return value && savedAnswers.get(slot.id) !== value ? [{ slotId: slot.id, value }] : [];
      });
    try {
      const saved = await schedulePollApi.respond(calendarId, pollId, answerEtag, answers);
      // Answering moves the poll's etag, so the poll is read again.
      await reload();
      setStale(false);
      toast.success(t('schedulePoll.answerSaved', { count: saved.answers.length }));
      synchronizeActions();
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          const latest = await reload();
          setDraftAnswers((saved) => ({
            ...saved,
            ...Object.fromEntries(
              answers
                .filter(({ slotId }) =>
                  latest.slots.some((slot) => slot.id === slotId && slot.active),
                )
                .map(({ slotId, value }) => [slotId, value]),
            ),
          }));
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else {
        toast.error(errorMessage(reason));
      }
    } finally {
      setSavingAnswers(false);
    }
  };

  const saveMembers = async () => {
    if (!poll) return;
    const initiallySelected = new Set((poll.members ?? []).map((member) => member.id));
    const requestedMemberIds = [...memberIdsDraft];
    setSavingMembers(true);
    try {
      const updated = await schedulePollApi.update(calendarId, pollId, poll.etag, {
        memberIds: memberIdsDraft,
      });
      setPoll(updated);
      setAcknowledged(false);
      setMemberIdsDraft((updated.members ?? []).map((member) => member.id));
      setDraftAnswers(
        Object.fromEntries(updated.myAnswers.map((answer) => [answer.slotId, answer.value])),
      );
      toast.success(t('schedulePoll.membersUpdated'));
      synchronizeActions();
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          const latest = await reload();
          // A failed refresh leaves the roster already on screen.
          await useCalendarStore.getState().fetchMembers(calendarId);
          const currentRoster = useCalendarStore.getState().membersMap[calendarId] ?? roster;
          const eligibleMemberIds = new Set(currentRoster.map((member) => member.id));
          const explicitlyRemoved = new Set(
            [...initiallySelected].filter((id) => !requestedMemberIds.includes(id)),
          );
          const mergedMemberIds = (latest.members ?? [])
            .map((member) => member.id)
            .filter((id) => !explicitlyRemoved.has(id));
          for (const id of requestedMemberIds) {
            if (
              !initiallySelected.has(id) &&
              (eligibleMemberIds.size === 0 || eligibleMemberIds.has(id))
            ) {
              mergedMemberIds.push(id);
            }
          }
          setMemberIdsDraft([...new Set(mergedMemberIds)]);
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    } finally {
      setSavingMembers(false);
    }
  };

  const saveCandidates = async () => {
    if (!poll) return;
    const initiallyActiveSlotIds = new Set(
      poll.slots.filter((slot) => slot.active).map((slot) => slot.id),
    );
    const requestedNewCandidates = [...newCandidates];
    const requestedKeptSlotIds = [...keptSlotIds];
    const slots: Array<{ id?: string; startAt: string; endAt: string }> = [
      ...poll.slots
        .filter((slot) => keptSlotIds.includes(slot.id) && slot.active)
        .map((slot) => ({ id: slot.id, startAt: slot.startAt, endAt: slot.endAt })),
      ...newCandidates.map((slot) => ({ startAt: slot.startAt, endAt: slot.endAt })),
    ];
    if (slots.length === 0) return;
    setSavingCandidates(true);
    try {
      const updated = await schedulePollApi.update(calendarId, pollId, poll.etag, { slots });
      setPoll(updated);
      setAcknowledged(false);
      setKeptSlotIds(updated.slots.filter((slot) => slot.active).map((slot) => slot.id));
      setNewCandidates([]);
      toast.success(t('schedulePoll.candidatesUpdated'));
      synchronizeActions();
      navigateView('manage');
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          const latest = await reload();
          const currentCandidateKeys = new Set(
            latest.slots.filter((slot) => slot.active).map((slot) => candidateKey(slot)),
          );
          const explicitlyRemovedSlotIds = new Set(
            [...initiallyActiveSlotIds].filter((id) => !requestedKeptSlotIds.includes(id)),
          );
          setKeptSlotIds(
            latest.slots
              .filter((slot) => slot.active && !explicitlyRemovedSlotIds.has(slot.id))
              .map((slot) => slot.id),
          );
          setNewCandidates(
            requestedNewCandidates.filter((slot) => !currentCandidateKeys.has(candidateKey(slot))),
          );
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    } finally {
      setSavingCandidates(false);
    }
  };

  const refreshAvailability = async () => {
    if (!poll) return;
    try {
      const updated = await schedulePollApi.refresh(calendarId, pollId);
      setPoll(updated);
      setAcknowledged(false);
      setKeptSlotIds(updated.slots.filter((slot) => slot.active).map((slot) => slot.id));
      setSelectedSlotId(
        updated.selectedSlotId ??
          updated.slots.find((slot) => slot.active && slot.availability !== 'hard')?.id ??
          '',
      );
      toast.success(t('schedulePoll.availabilityRefreshed'));
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          await reload();
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    }
  };

  const finalize = async () => {
    if (!poll || !selectedSlotId) return;
    setFinalizing(true);
    try {
      const finalized = await schedulePollApi.finalize(calendarId, pollId, poll.etag, {
        slotId: selectedSlotId,
        acknowledged,
        ...(ownerId ? { ownerId } : {}),
      });
      setPoll(finalized);
      synchronizeActions();
      toast.success(t('schedulePoll.finalized'));
      if (finalized.confirmedEventId) navigateView('event');
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          await reload(true);
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    } finally {
      setFinalizing(false);
    }
  };

  const cancelPoll = async () => {
    if (!poll) return;
    setCancelling(true);
    try {
      const cancelled = await schedulePollApi.cancel(calendarId, pollId, poll.etag);
      setPoll(cancelled);
      synchronizeActions();
      toast.success(t('schedulePoll.cancelled'));
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          await reload();
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    } finally {
      setCancelling(false);
      setConfirmingCancel(false);
    }
  };

  const activeSlots = useMemo(
    () => (poll?.slots ?? []).filter((slot) => slot.active && slot.availability !== 'hard'),
    [poll?.slots],
  );
  const answerCount = activeSlots.filter((slot) => !!draftAnswers[slot.id]).length;
  const hasDraftChanges =
    !!poll &&
    activeSlots.some((slot) => {
      const saved = poll.myAnswers.find((answer) => answer.slotId === slot.id)?.value;
      return (draftAnswers[slot.id] ?? undefined) !== saved;
    });
  const unsavedGuard = useUnsavedGuard(hasDraftChanges);
  const pendingCount = poll ? Math.max(0, poll.memberCount - poll.respondedCount) : 0;
  const hasRecipients = !!poll && poll.memberCount > 0;
  const responseFor = (memberId: string, slotId: string) =>
    poll?.responses
      ?.find((response) => response.memberId === memberId)
      ?.answers.find((answer) => answer.slotId === slotId)?.value;
  const selectedSlot = poll?.slots.find((slot) => slot.id === selectedSlotId);
  const selectedResponses =
    poll?.responses?.flatMap((response) => {
      const answer = response.answers.find((entry) => entry.slotId === selectedSlotId);
      return answer ? [answer.value] : [];
    }) ?? [];
  const slotHasNo = selectedResponses.includes('no');
  const requiresAcknowledgment =
    selectedSlot?.availability === 'soft' || selectedResponses.includes('maybe');
  const hasNoUsableCandidates = !!poll && poll.status === 'open' && activeSlots.length === 0;
  const currentRoster = roster.length > 0 ? roster : (poll?.members ?? []);
  const eventDisplayTitle = event?.title ?? poll?.title ?? t('schedulePoll.eventTitle');
  const eventVisibilityKey = event
    ? event.visibility === 'public'
      ? 'event.visibilityPublic'
      : event.visibility === 'private'
        ? 'event.visibilityPrivate'
        : event.visibility === 'confidential'
          ? 'event.visibilityConfidential'
          : 'event.visibilityDefault'
    : 'event.visibilityConfidential';
  const eventVisibilityHintKey = event
    ? event.visibility === 'public'
      ? 'schedulePoll.visibilityPublicHint'
      : event.visibility === 'private'
        ? 'schedulePoll.visibilityPrivateHint'
        : event.visibility === 'confidential'
          ? 'schedulePoll.visibilityConfidentialHint'
          : 'schedulePoll.visibilityDefaultHint'
    : 'schedulePoll.visibilityConfidentialHint';

  const tallyFor = (slotId: string) => {
    const tally = { yes: 0, maybe: 0, no: 0 };
    for (const response of poll?.responses ?? []) {
      const value = response.answers.find((answer) => answer.slotId === slotId)?.value;
      if (value) tally[value] += 1;
    }
    return tally;
  };
  const respondedMemberIds = new Set(
    (poll?.responses ?? [])
      .filter((response) => response.answers.length > 0)
      .map((response) => response.memberId),
  );
  const bestSlotId = (() => {
    let best: { id: string; yes: number; maybe: number } | null = null;
    for (const slot of poll?.slots ?? []) {
      if (!slot.active || slot.availability === 'hard') continue;
      const tally = tallyFor(slot.id);
      if (tally.no > 0 || tally.yes === 0) continue;
      if (!best || tally.yes > best.yes || (tally.yes === best.yes && tally.maybe < best.maybe)) {
        best = { id: slot.id, yes: tally.yes, maybe: tally.maybe };
      }
    }
    return best?.id;
  })();

  if (loading) {
    return (
      <SchedulePollFrame title={t('schedulePoll.title')}>
        <div className="flex justify-center py-16 text-[var(--color-text-tertiary)]">
          <span role="status" aria-label={t('common.loading')} className="spinner h-5 w-5" />
        </div>
      </SchedulePollFrame>
    );
  }

  if (loadError || !poll) {
    return (
      <SchedulePollFrame title={t('schedulePoll.title')}>
        <SchedulePollNotice tone="error">
          {loadError ?? t('schedulePoll.unavailablePoll')}
        </SchedulePollNotice>
      </SchedulePollFrame>
    );
  }

  if (view === 'event') {
    return (
      <SchedulePollFrame title={eventDisplayTitle} eyebrow={t('schedulePoll.confirmedEvent')}>
        {!poll.confirmedEventId ? (
          <SchedulePollNotice>{t('schedulePoll.eventUnavailable')}</SchedulePollNotice>
        ) : eventError ? (
          <SchedulePollNotice tone={eventError === 'missing' ? 'warning' : 'error'}>
            {t(
              eventError === 'missing'
                ? 'schedulePoll.eventMissing'
                : 'schedulePoll.eventAccessUnavailable',
            )}
          </SchedulePollNotice>
        ) : event ? (
          <section className="page-card max-w-[640px] p-5 sm:p-6">
            <SchedulePollStatus status={poll.status} />
            <h2 className="mt-2 text-title font-bold text-[var(--color-text-primary)]">
              {event.title}
            </h2>
            <div className="mt-4">
              <SchedulePollSlot
                startAt={event.startAt}
                endAt={event.endAt}
                timezone={event.timezone ?? poll.timezone}
              />
            </div>
            {event.location && (
              <p className="mt-4 flex items-start gap-2 text-body text-[var(--color-text-primary)]">
                <svg
                  aria-hidden
                  width="16"
                  height="16"
                  viewBox="0 0 16 16"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.5"
                  className="mt-0.5 shrink-0 text-[var(--color-text-tertiary)]"
                >
                  <path d="M8 14s4.5-4.1 4.5-7.5a4.5 4.5 0 0 0-9 0C3.5 9.9 8 14 8 14Z" />
                  <circle cx="8" cy="6.5" r="1.5" />
                </svg>
                {event.location}
              </p>
            )}
            <div className="mt-5 rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-4 py-3">
              <p className="text-footnote font-semibold text-[var(--color-text-primary)]">
                {t('schedulePoll.visibilitySummary')}: {t(eventVisibilityKey)}
              </p>
              <p className="mt-0.5 text-footnote text-[var(--color-text-secondary)]">
                {t(eventVisibilityHintKey)}
              </p>
            </div>
            <Link
              to="/schedule-polls/$calendarId/$pollId"
              params={{ calendarId, pollId }}
              search={{ view: poll.canManage ? 'manage' : 'respond' }}
              className={`${SECONDARY_ACTION} mt-5`}
            >
              {t('schedulePoll.backToPoll')}
            </Link>
          </section>
        ) : null}
      </SchedulePollFrame>
    );
  }

  const pollMembers = poll.members ?? [];
  const showTabs = poll.status === 'open' && poll.canRespond && poll.canManage;
  const takeOver = async () => {
    try {
      const updated = await schedulePollApi.update(calendarId, pollId, poll.etag, {
        takeOver: true,
      });
      setPoll(updated);
      setAcknowledged(false);
      toast.success(t('schedulePoll.takenOver'));
      synchronizeActions();
    } catch (reason) {
      if (isStaleError(reason)) {
        try {
          await reload();
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    }
  };
  const inviteesChanged = !(
    memberIdsDraft.length === pollMembers.length &&
    pollMembers.every((member) => memberIdsDraft.includes(member.id))
  );

  return (
    <SchedulePollFrame title={poll.title} eyebrow={t('schedulePoll.eyebrow')}>
      <ConfirmSheet
        open={unsavedGuard.blocked}
        title={t('common.discardTitle')}
        confirmLabel={t('common.discard')}
        cancelLabel={t('common.keepEditing')}
        onConfirm={unsavedGuard.proceed}
        onCancel={unsavedGuard.stay}
      />
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          <SchedulePollStatus status={poll.status} />
          <SchedulePollProgress responded={poll.respondedCount} members={poll.memberCount} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {poll.canTakeOver && poll.status === 'open' && (
            <button type="button" onClick={takeOver} className={ACCENT_ACTION}>
              {t('schedulePoll.takeOver')}
            </button>
          )}
          {showTabs && (
            <div className="segmented-control w-full sm:w-auto sm:min-w-[240px]">
              <button
                type="button"
                onClick={() => navigateView('respond')}
                data-active={view === 'respond'}
                aria-current={view === 'respond' ? 'page' : undefined}
              >
                {t('schedulePoll.respond')}
              </button>
              <button
                type="button"
                onClick={() => navigateView('manage')}
                data-active={view === 'manage' || view === 'candidates'}
                aria-current={view === 'manage' ? 'page' : undefined}
              >
                {t('schedulePoll.manage')}
              </button>
            </div>
          )}
        </div>
      </div>

      {(refreshWarning || stale) && (
        <div className="flex flex-col gap-2">
          {refreshWarning && (
            <SchedulePollNotice tone="warning">{refreshWarning}</SchedulePollNotice>
          )}
          {stale && <SchedulePollNotice tone="warning">{t('conflict.stale')}</SchedulePollNotice>}
        </div>
      )}

      {view === 'respond' && (
        <SchedulePollSection
          title={t('schedulePoll.respond')}
          hint={t('schedulePoll.answerInstruction')}
          className="max-w-[820px]"
        >
          {poll.status !== 'open' ? (
            <SchedulePollNotice>{t('schedulePoll.pollClosed')}</SchedulePollNotice>
          ) : hasNoUsableCandidates ? (
            <SchedulePollNotice tone="warning">
              {t('schedulePoll.candidatesRevising')}
            </SchedulePollNotice>
          ) : !poll.canRespond ? (
            <SchedulePollNotice tone="error">
              {t('schedulePoll.noResponseAccess')}
            </SchedulePollNotice>
          ) : (
            <>
              <div className="page-card divide-y divide-[var(--color-separator)]">
                {activeSlots.map((slot) => (
                  <AnswerSlot
                    key={slot.id}
                    slot={slot}
                    timezone={poll.timezone}
                    value={draftAnswers[slot.id]}
                    onChange={(value) =>
                      setDraftAnswers((previous) => ({ ...previous, [slot.id]: value }))
                    }
                  />
                ))}
              </div>
              {poll.slots.some((slot) => !slot.active || slot.availability === 'hard') && (
                <div className="mt-3">
                  <SchedulePollNotice tone="warning">
                    {t('schedulePoll.someCandidatesUnavailable')}
                  </SchedulePollNotice>
                </div>
              )}
              <div className="page-card sticky bottom-4 z-10 mt-4 flex items-center justify-between gap-3 px-4 py-3 shadow-[var(--shadow-md)] sm:px-5">
                <p
                  role="status"
                  className="flex min-w-0 items-center gap-2 text-footnote text-[var(--color-text-secondary)] tabular-nums"
                >
                  <span
                    aria-hidden
                    className={`h-1.5 w-1.5 shrink-0 rounded-full ${hasDraftChanges ? 'bg-[var(--color-warning)]' : 'bg-[var(--color-success)]'}`}
                  />
                  <span className="truncate">
                    {t('schedulePoll.answerProgress', {
                      answered: answerCount,
                      total: activeSlots.length,
                    })}
                    {' · '}
                    {hasDraftChanges ? t('schedulePoll.unsaved') : t('schedulePoll.saved')}
                  </span>
                </p>
                <button
                  type="button"
                  onClick={saveResponse}
                  disabled={savingAnswers || !hasDraftChanges || activeSlots.length === 0}
                  className="btn-primary h-10 shrink-0 px-5 text-default"
                >
                  {savingAnswers ? t('common.saving') : t('schedulePoll.saveAnswers')}
                </button>
              </div>
            </>
          )}
        </SchedulePollSection>
      )}

      {view === 'manage' &&
        (!poll.canManage ? (
          <SchedulePollNotice tone="error">{t('schedulePoll.noManageAccess')}</SchedulePollNotice>
        ) : (
          <>
            <SchedulePollSection
              title={t('schedulePoll.manage')}
              hint={t('schedulePoll.managerProgress', {
                responded: poll.respondedCount,
                members: poll.memberCount,
              })}
              actions={
                poll.status === 'open' ? (
                  <>
                    <button
                      type="button"
                      onClick={() => navigateView('candidates')}
                      className={SECONDARY_ACTION}
                    >
                      <svg
                        aria-hidden
                        width="13"
                        height="13"
                        viewBox="0 0 16 16"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                      >
                        <path d="M8 3v10M3 8h10" />
                      </svg>
                      {t('schedulePoll.editCandidates')}
                    </button>
                    <button
                      type="button"
                      onClick={refreshAvailability}
                      className={SECONDARY_ACTION}
                    >
                      <svg
                        aria-hidden
                        width="13"
                        height="13"
                        viewBox="0 0 16 16"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="1.8"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="M13 8a5 5 0 1 1-1.5-3.55M13 2.5v2.5h-2.5" />
                      </svg>
                      {t('schedulePoll.refreshAvailability')}
                    </button>
                  </>
                ) : undefined
              }
            >
              {poll.slots.length === 0 ? (
                <SchedulePollNotice tone="warning">
                  {t('schedulePoll.candidatesRevising')}
                </SchedulePollNotice>
              ) : (
                <>
                  <div className="page-card hidden overflow-x-auto sm:block">
                    <table className="w-full border-collapse text-left">
                      <thead>
                        <tr className="border-b border-[var(--color-separator)]">
                          <th
                            scope="col"
                            className="w-px whitespace-nowrap px-5 py-3 text-footnote font-medium text-[var(--color-text-secondary)]"
                          >
                            {t('schedulePoll.candidate')}
                          </th>
                          <th
                            scope="col"
                            className="w-px whitespace-nowrap px-3 py-3 text-footnote font-medium text-[var(--color-text-secondary)]"
                          >
                            {t('schedulePoll.tally')}
                          </th>
                          {pollMembers.map((member) => (
                            <th
                              key={member.id}
                              scope="col"
                              className="min-w-[80px] px-2 py-3 text-center align-bottom"
                            >
                              <span
                                className={`flex flex-col items-center gap-1 ${respondedMemberIds.has(member.id) ? '' : 'opacity-50'}`}
                              >
                                <Avatar
                                  name={member.name}
                                  size="h-7 w-7"
                                  round
                                  className="text-footnote"
                                />
                                <span className="block max-w-[84px] truncate text-micro font-medium text-[var(--color-text-secondary)]">
                                  {member.name}
                                </span>
                              </span>
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[var(--color-separator)]">
                        {poll.slots.map((slot) => {
                          const tally = tallyFor(slot.id);
                          const best = slot.id === bestSlotId;
                          return (
                            <tr
                              key={slot.id}
                              className={`${!slot.active ? 'opacity-50' : ''} ${best ? 'bg-[color-mix(in_srgb,var(--color-accent)_6%,transparent)]' : ''}`}
                            >
                              <th scope="row" className="whitespace-nowrap px-5 py-2.5 font-normal">
                                <span className="flex items-center gap-3">
                                  <SchedulePollSlot
                                    startAt={slot.startAt}
                                    endAt={slot.endAt}
                                    timezone={poll.timezone}
                                    dense
                                  />
                                  {best && <BestTag />}
                                  {slot.availability !== 'open' && (
                                    <AvailabilityBadge availability={slot.availability} />
                                  )}
                                  {!slot.active && (
                                    <span className="text-micro text-[var(--color-text-tertiary)]">
                                      {t('schedulePoll.unavailable')}
                                    </span>
                                  )}
                                </span>
                              </th>
                              <td className="whitespace-nowrap px-3 py-2.5">
                                <Tally tally={tally} />
                              </td>
                              {pollMembers.map((member) => {
                                const value = responseFor(member.id, slot.id);
                                return (
                                  <td key={member.id} className="px-2 py-2.5 text-center">
                                    <span className="inline-flex" title={answerLabel(t, value)}>
                                      <AnswerMark answer={value} size={18} />
                                      <span className="sr-only">{answerLabel(t, value)}</span>
                                    </span>
                                  </td>
                                );
                              })}
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>

                  <ul className="page-card divide-y divide-[var(--color-separator)] sm:hidden">
                    {poll.slots.map((slot) => {
                      const best = slot.id === bestSlotId;
                      return (
                        <li
                          key={slot.id}
                          className={`px-4 py-3 ${!slot.active ? 'opacity-50' : ''} ${best ? 'bg-[color-mix(in_srgb,var(--color-accent)_6%,transparent)]' : ''}`}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <SchedulePollSlot
                              startAt={slot.startAt}
                              endAt={slot.endAt}
                              timezone={poll.timezone}
                              dense
                            />
                            <span className="flex shrink-0 flex-col items-end gap-1">
                              {best && <BestTag />}
                              {slot.availability !== 'open' && (
                                <AvailabilityBadge availability={slot.availability} />
                              )}
                            </span>
                          </div>
                          <div className="mt-2.5 flex items-center justify-between gap-3">
                            <ul className="flex min-w-0 flex-wrap gap-x-3 gap-y-1.5">
                              {pollMembers.map((member) => {
                                const value = responseFor(member.id, slot.id);
                                return (
                                  <li
                                    key={member.id}
                                    className="flex items-center gap-1 text-footnote text-[var(--color-text-secondary)]"
                                  >
                                    <AnswerMark answer={value} size={14} />
                                    <span className="max-w-[96px] truncate">{member.name}</span>
                                    <span className="sr-only">{answerLabel(t, value)}</span>
                                  </li>
                                );
                              })}
                            </ul>
                            <Tally tally={tallyFor(slot.id)} />
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                </>
              )}

              {hasNoUsableCandidates && (
                <div className="mt-3">
                  <SchedulePollNotice tone="warning">
                    {t('schedulePoll.addCandidatesRequired')}
                  </SchedulePollNotice>
                </div>
              )}
            </SchedulePollSection>

            {poll.status === 'open' && (
              <SchedulePollSection
                title={t('schedulePoll.finalize')}
                hint={
                  pendingCount > 0
                    ? t('schedulePoll.waitingForResponses', { count: pendingCount })
                    : undefined
                }
              >
                <div className="page-card p-4 sm:p-5">
                  {activeSlots.length === 0 && (
                    <p className="mb-3 text-footnote text-[var(--color-warning)]">
                      {t('schedulePoll.addCandidatesRequired')}
                    </p>
                  )}
                  <fieldset className="grid gap-2 sm:grid-cols-2">
                    <legend className="sr-only">{t('schedulePoll.selectFinalCandidate')}</legend>
                    {activeSlots.map((slot) => {
                      const tally = tallyFor(slot.id);
                      const blocked = tally.no > 0;
                      const hasMaybe = slot.availability === 'soft' || tally.maybe > 0;
                      return (
                        <label
                          key={slot.id}
                          className={`choice-row flex min-h-14 cursor-pointer items-center gap-3 px-3 py-2 ${blocked ? 'opacity-55' : ''}`}
                        >
                          <input
                            type="radio"
                            name="final-slot"
                            value={slot.id}
                            checked={selectedSlotId === slot.id}
                            disabled={blocked}
                            onChange={() => {
                              setSelectedSlotId(slot.id);
                              setAcknowledged(false);
                            }}
                            className="check-input"
                          />
                          <span className="min-w-0 flex-1">
                            <SchedulePollSlot
                              startAt={slot.startAt}
                              endAt={slot.endAt}
                              timezone={poll.timezone}
                              dense
                            />
                          </span>
                          <span className="flex shrink-0 flex-col items-end gap-1">
                            <Tally tally={tally} />
                            {blocked ? (
                              <span className="text-micro font-semibold text-[var(--color-danger)]">
                                {t('schedulePoll.blockedByNo')}
                              </span>
                            ) : hasMaybe ? (
                              <span className="text-micro font-medium text-[var(--color-warning)]">
                                {t('schedulePoll.needsAcknowledgment')}
                              </span>
                            ) : null}
                          </span>
                        </label>
                      );
                    })}
                  </fieldset>

                  <div className="mt-5 grid gap-4 border-t border-[var(--color-separator)] pt-5 sm:grid-cols-2">
                    <div className="text-footnote font-medium text-[var(--color-text-secondary)]">
                      <span className="mb-1.5 block">{t('schedulePoll.eventOwner')}</span>
                      <CustomSelect
                        value={ownerId}
                        onChange={setOwnerId}
                        ariaLabel={t('schedulePoll.eventOwner')}
                        className="w-full"
                        triggerClassName="input-modern"
                        options={pollMembers.map((member) => {
                          const full = calendarMembers.find((m) => m.id === member.id);
                          return full
                            ? memberOption(full, userId, t)
                            : { value: member.id, label: member.name };
                        })}
                      />
                    </div>
                    <div className="self-end rounded-[var(--radius-md)] bg-[var(--color-surface-inset)] px-4 py-3">
                      <p className="text-footnote font-semibold text-[var(--color-text-primary)]">
                        {t('schedulePoll.visibilitySummary')}
                      </p>
                      <p className="mt-0.5 text-footnote text-[var(--color-text-secondary)]">
                        {t('schedulePoll.confidentialVisibilityHint')}
                      </p>
                    </div>
                  </div>

                  {requiresAcknowledgment && selectedSlot && (
                    <label className="callout callout-warning mt-4 cursor-pointer text-footnote text-[var(--color-text-primary)]">
                      <input
                        type="checkbox"
                        checked={acknowledged}
                        onChange={(event) => setAcknowledged(event.target.checked)}
                        className="check-input mt-px"
                      />
                      <span>{t('schedulePoll.acknowledgeSoftSlot')}</span>
                    </label>
                  )}
                  {slotHasNo && selectedSlot && (
                    <p role="alert" className="mt-3 text-footnote text-[var(--color-danger)]">
                      {t('schedulePoll.cannotFinalizeNo')}
                    </p>
                  )}
                  <div className="mt-5 flex justify-end">
                    <button
                      type="button"
                      onClick={() => (pendingCount > 0 ? setConfirmingPending(true) : finalize())}
                      disabled={
                        finalizing ||
                        !hasRecipients ||
                        !selectedSlotId ||
                        slotHasNo ||
                        (requiresAcknowledgment && !acknowledged)
                      }
                      className="btn-primary w-full px-6 text-default sm:w-auto"
                    >
                      {finalizing ? t('common.saving') : t('schedulePoll.finalizeAndCreateEvent')}
                    </button>
                  </div>
                  <ConfirmSheet
                    open={confirmingPending}
                    title={t('poll.finalizeWithoutPending')}
                    confirmLabel={t('schedulePoll.finalizeAndCreateEvent')}
                    destructive={false}
                    busy={finalizing}
                    onConfirm={async () => {
                      await finalize();
                      setConfirmingPending(false);
                    }}
                    onCancel={() => setConfirmingPending(false)}
                  />
                </div>
              </SchedulePollSection>
            )}

            {poll.status === 'open' && (
              <details className="page-card group">
                <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3.5 sm:px-5 [&::-webkit-details-marker]:hidden">
                  <span className="min-w-0">
                    <span className="block text-default font-semibold text-[var(--color-text-primary)]">
                      {t('schedulePoll.invitees', { count: memberIdsDraft.length })}
                    </span>
                    <span className="mt-0.5 block text-footnote text-[var(--color-text-secondary)]">
                      {t('schedulePoll.inviteesHint')}
                    </span>
                  </span>
                  <svg
                    aria-hidden
                    width="14"
                    height="14"
                    viewBox="0 0 14 14"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.8"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className="shrink-0 text-[var(--color-text-tertiary)] transition-transform group-open:rotate-90"
                  >
                    <path d="m5 2.5 4.5 4.5L5 11.5" />
                  </svg>
                </summary>
                <div className="border-t border-[var(--color-separator)] px-4 py-4 sm:px-5">
                  <InviteeGrid
                    members={currentRoster}
                    selectedIds={memberIdsDraft}
                    onToggle={(memberId, checked) =>
                      setMemberIdsDraft((previous) =>
                        checked
                          ? [...new Set([...previous, memberId])]
                          : previous.filter((id) => id !== memberId),
                      )
                    }
                  />
                  <div className="mt-4 flex items-center justify-between gap-3">
                    <p role="alert" className="text-footnote text-[var(--color-danger)]">
                      {memberIdsDraft.length === 0 ? t('schedulePoll.selectAtLeastOne') : ''}
                    </p>
                    <button
                      type="button"
                      onClick={saveMembers}
                      disabled={savingMembers || memberIdsDraft.length === 0 || !inviteesChanged}
                      className="btn-primary h-9 shrink-0 px-4 text-body"
                    >
                      {savingMembers ? t('common.saving') : t('schedulePoll.saveInvitees')}
                    </button>
                  </div>
                </div>
              </details>
            )}

            {poll.status === 'open' && (
              <div className="flex justify-center sm:justify-start">
                <button
                  type="button"
                  onClick={() => setConfirmingCancel(true)}
                  disabled={cancelling}
                  className="rounded-full px-3 py-2 text-footnote font-medium text-[var(--color-danger)] transition hover:bg-[var(--color-danger-bg)] disabled:opacity-50"
                >
                  {cancelling ? t('common.saving') : t('schedulePoll.cancelPoll')}
                </button>
                <ConfirmSheet
                  open={confirmingCancel}
                  title={t('confirm.cancelPollTitle')}
                  hint={t('schedulePoll.cancelConfirm', { title: poll.title })}
                  confirmLabel={t('confirm.cancelPoll')}
                  busy={cancelling}
                  onConfirm={cancelPoll}
                  onCancel={() => setConfirmingCancel(false)}
                />
              </div>
            )}
          </>
        ))}

      {view === 'candidates' &&
        (!poll.canManage ? (
          <SchedulePollNotice tone="error">{t('schedulePoll.noManageAccess')}</SchedulePollNotice>
        ) : poll.status !== 'open' ? (
          <SchedulePollNotice>{t('schedulePoll.pollClosed')}</SchedulePollNotice>
        ) : (
          <>
            <SchedulePollSection
              title={t('schedulePoll.editCandidates')}
              hint={t('schedulePoll.candidateEditHint')}
              actions={
                <button
                  type="button"
                  onClick={() => navigateView('manage')}
                  className={SECONDARY_ACTION}
                >
                  {t('schedulePoll.backToManage')}
                </button>
              }
            >
              <div className="page-card p-4 sm:p-5">
                <h3 className="mb-3 text-default font-semibold text-[var(--color-text-primary)]">
                  {t('schedulePoll.currentCandidates')}
                </h3>
                {poll.slots.length === 0 ? (
                  <p className="text-footnote text-[var(--color-text-secondary)]">
                    {t('schedulePoll.noCandidates')}
                  </p>
                ) : (
                  <ul className="grid gap-2 sm:grid-cols-2">
                    {poll.slots.map((slot) => (
                      <li key={slot.id}>
                        <label
                          className={`choice-row flex min-h-14 cursor-pointer items-center gap-3 px-3 py-2 ${!slot.active ? 'opacity-55' : ''}`}
                        >
                          <input
                            type="checkbox"
                            checked={keptSlotIds.includes(slot.id)}
                            disabled={!slot.active}
                            onChange={(event) =>
                              setKeptSlotIds((previous) =>
                                event.target.checked
                                  ? [...previous, slot.id]
                                  : previous.filter((id) => id !== slot.id),
                              )
                            }
                            aria-label={t('schedulePoll.keepCandidate', {
                              candidate: formatSchedulePollSlotTime(
                                slot.startAt,
                                slot.endAt,
                                poll.timezone,
                                locale,
                              ),
                            })}
                            className="check-input"
                          />
                          <span className="min-w-0 flex-1">
                            <SchedulePollSlot
                              startAt={slot.startAt}
                              endAt={slot.endAt}
                              timezone={poll.timezone}
                              dense
                            />
                          </span>
                          {(slot.availability !== 'open' || !slot.active) && (
                            <AvailabilityBadge
                              availability={slot.active ? slot.availability : 'hard'}
                            />
                          )}
                        </label>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            </SchedulePollSection>
            <SchedulePollCandidateFinder
              calendarId={calendarId}
              memberIds={memberIdsDraft}
              selected={newCandidates}
              onChange={setNewCandidates}
            />
            <div className="page-card sticky bottom-4 z-10 flex items-center justify-between gap-3 px-4 py-3 shadow-[var(--shadow-md)] sm:px-5">
              <p className="min-w-0 text-footnote text-[var(--color-text-secondary)] tabular-nums">
                {t('schedulePoll.candidateSaveReview', {
                  kept: keptSlotIds.length,
                  added: newCandidates.length,
                })}
              </p>
              <button
                type="button"
                onClick={saveCandidates}
                disabled={savingCandidates || keptSlotIds.length + newCandidates.length === 0}
                className="btn-primary h-10 shrink-0 px-5 text-default"
              >
                {savingCandidates ? t('common.saving') : t('schedulePoll.saveCandidates')}
              </button>
            </div>
          </>
        ))}

      {poll.status === 'finalized' &&
        (poll.confirmedEventId ? (
          <div>
            <Link
              to="/schedule-polls/$calendarId/$pollId"
              params={{ calendarId, pollId }}
              search={{ view: 'event' }}
              className={ACCENT_ACTION}
            >
              {t('schedulePoll.viewEvent')}
            </Link>
          </div>
        ) : (
          <SchedulePollNotice>{t('schedulePoll.eventUnavailable')}</SchedulePollNotice>
        ))}
    </SchedulePollFrame>
  );
}

const SECONDARY_ACTION =
  'inline-flex h-8 items-center gap-1.5 rounded-full bg-[var(--color-surface-inset)] px-3.5 text-footnote font-semibold text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)]';
const ACCENT_ACTION =
  'inline-flex h-8 items-center gap-1.5 rounded-full bg-[var(--color-accent-bg)] px-3.5 text-footnote font-semibold text-[var(--color-accent-ink)] transition hover:bg-[var(--color-accent-subtle)]';

function BestTag() {
  const t = useT();
  return (
    <span className="inline-flex shrink-0 items-center rounded-full bg-[var(--color-accent)] px-2 py-0.5 text-micro font-semibold text-[var(--color-text-on-accent)]">
      {t('schedulePoll.bestCandidate')}
    </span>
  );
}

function Tally({ tally }: { tally: { yes: number; maybe: number; no: number } }) {
  const t = useT();
  return (
    <span
      className="inline-flex shrink-0 items-center gap-2 text-footnote font-semibold text-[var(--color-text-primary)] tabular-nums"
      title={t('schedulePoll.answerTally', tally)}
    >
      <span className="sr-only">{t('schedulePoll.answerTally', tally)}</span>
      {(['yes', 'maybe', 'no'] as const).map((key) => (
        <span
          key={key}
          aria-hidden
          className={`inline-flex items-center gap-0.5 ${tally[key] === 0 ? 'opacity-35' : ''}`}
        >
          <AnswerMark answer={key} size={12} />
          {tally[key]}
        </span>
      ))}
    </span>
  );
}

function AnswerSlot({
  slot,
  timezone,
  value,
  onChange,
}: {
  slot: PollSlot;
  timezone: string;
  value: PollAnswer | undefined;
  onChange: (value: PollAnswer) => void;
}) {
  const t = useT();
  const locale = useUiStore((state) => state.locale);
  const candidateLabel = formatSchedulePollSlotTime(slot.startAt, slot.endAt, timezone, locale);
  const choices: Array<{
    value: PollAnswer;
    mark:
      | 'schedulePoll.answerYesMark'
      | 'schedulePoll.answerMaybeMark'
      | 'schedulePoll.answerNoMark';
    label: 'schedulePoll.answerYes' | 'schedulePoll.answerMaybe' | 'schedulePoll.answerNo';
  }> = [
    { value: 'yes', mark: 'schedulePoll.answerYesMark', label: 'schedulePoll.answerYes' },
    { value: 'maybe', mark: 'schedulePoll.answerMaybeMark', label: 'schedulePoll.answerMaybe' },
    { value: 'no', mark: 'schedulePoll.answerNoMark', label: 'schedulePoll.answerNo' },
  ];
  return (
    <fieldset className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:gap-4 sm:px-5">
      <legend className="sr-only">{t('schedulePoll.answerForCandidate')}</legend>
      <div className="flex min-w-0 flex-1 items-center justify-between gap-2">
        <SchedulePollSlot startAt={slot.startAt} endAt={slot.endAt} timezone={timezone} />
        {slot.availability !== 'open' && <AvailabilityBadge availability={slot.availability} />}
      </div>
      <div className="answer-seg w-full sm:w-[360px]">
        {choices.map((choice) => (
          <label key={choice.value} data-answer={choice.value} className="answer-opt">
            <input
              className="sr-only"
              type="radio"
              name={`answer-${slot.id}`}
              value={choice.value}
              checked={value === choice.value}
              onChange={() => onChange(choice.value)}
              aria-label={`${candidateLabel}: ${t(choice.mark)} ${t(choice.label)}`}
            />
            <AnswerMark answer={choice.value} size={15} />
            <span className="min-w-0 truncate">{t(choice.label)}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
