import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useMemo, useState } from 'react';
import { CustomSelect } from '@/components/pickers';
import { SchedulePollCandidateFinder } from '@/components/schedule-poll-candidate-finder';
import {
  AvailabilityBadge,
  formatSchedulePollSlotTime,
  SchedulePollFrame,
  SchedulePollNotice,
  SchedulePollSlotTime,
  SchedulePollStatus,
} from '@/components/schedule-poll-ui';
import { useT } from '@/i18n';
import { ApiError, errorMessage } from '@/lib/api';
import {
  type PollAnswer,
  type PollMemberListItem,
  type PollSlot,
  type SchedulePoll,
  type SchedulePollCandidate,
  schedulePollApi,
} from '@/lib/schedule-polls';
import { toast } from '@/lib/toast';
import { useAuthStore } from '@/stores/auth-store';
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

function answerTone(answer: PollAnswer | undefined): string {
  if (answer === 'yes') return 'text-[var(--color-accent)]';
  if (answer === 'maybe') return 'text-[var(--color-warning,#e5a00d)]';
  if (answer === 'no') return 'text-[var(--color-danger)]';
  return 'text-[var(--color-text-tertiary)]';
}

function candidateKey(slot: Pick<PollSlot, 'startAt' | 'endAt'>): string {
  return `${Date.parse(slot.startAt)}/${Date.parse(slot.endAt)}`;
}

function staleError(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    (error.status === 409 ||
      error.code === 'SCHEDULE_POLL.STALE' ||
      error.code === 'SCHEDULE_POLL.CONFLICT')
  );
}

function SchedulePollDetailPage() {
  const t = useT();
  const navigate = useNavigate();
  const { calendarId, pollId } = Route.useParams();
  const { view: requestedView } = Route.useSearch();
  const userId = useAuthStore((state) => state.user?.id);
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
  const [roster, setRoster] = useState<PollMemberListItem[]>([]);
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

  const view: PollView = requestedView ?? (poll?.canManage ? 'manage' : 'respond');
  const reload = async (refresh = false) => {
    const current = await schedulePollApi.get(calendarId, pollId);
    let latest = current;
    setRefreshWarning(null);
    if (refresh && current.status === 'open') {
      try {
        latest = await schedulePollApi.refresh(calendarId, pollId);
      } catch (reason) {
        if (staleError(reason)) {
          latest = await schedulePollApi.get(calendarId, pollId);
          setRefreshWarning(t('schedulePoll.staleReloaded'));
        } else {
          setRefreshWarning(errorMessage(reason));
        }
      }
    }
    setPoll(latest);
    setDraftAnswers(
      Object.fromEntries(latest.myAnswers.map((answer) => [answer.slotId, answer.value])),
    );
    setMemberIdsDraft(latest.members.map((member) => member.id));
    setKeptSlotIds(latest.slots.filter((slot) => slot.active).map((slot) => slot.id));
    setSelectedSlotId((currentSlot) =>
      latest.slots.some((slot) => slot.id === currentSlot && slot.active)
        ? currentSlot
        : (latest.selectedSlotId ??
          latest.slots.find((slot) => slot.active && slot.availability !== 'hard')?.id ??
          ''),
    );
    setOwnerId((currentOwner) =>
      latest.members.some((member) => member.id === currentOwner)
        ? currentOwner
        : (latest.members.find((member) => member.id === userId)?.id ??
          latest.members[0]?.id ??
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
            if (staleError(reason))
              latest = await schedulePollApi.get(calendarId, pollId, controller.signal);
            else if (active) setRefreshWarning(errorMessage(reason));
          }
        }
        if (!active) return;
        setPoll(latest);
        setDraftAnswers(
          Object.fromEntries(latest.myAnswers.map((answer) => [answer.slotId, answer.value])),
        );
        setMemberIdsDraft(latest.members.map((member) => member.id));
        setKeptSlotIds(latest.slots.filter((slot) => slot.active).map((slot) => slot.id));
        setSelectedSlotId(
          latest.selectedSlotId ??
            latest.slots.find((slot) => slot.active && slot.availability !== 'hard')?.id ??
            '',
        );
        setOwnerId(
          latest.members.find((member) => member.id === userId)?.id ?? latest.members[0]?.id ?? '',
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

  useEffect(() => {
    if (!poll?.canManage || poll.status !== 'open' || view === 'event') {
      setRoster([]);
      return;
    }
    const controller = new AbortController();
    schedulePollApi
      .members(calendarId, controller.signal)
      .then(setRoster)
      .catch(() => {
        if (!controller.signal.aborted) setRoster([]);
      });
    return () => controller.abort();
  }, [calendarId, poll?.canManage, poll?.status, view]);

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
    if (poll?.status !== 'open') return;
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
      const updated = await schedulePollApi.respond(calendarId, pollId, {
        version: poll.version,
        responseVersion: poll.responseVersion,
        answers,
      });
      setPoll(updated);
      if (updated.reviewVersion !== poll.reviewVersion) setAcknowledged(false);
      setDraftAnswers(
        Object.fromEntries(updated.myAnswers.map((answer) => [answer.slotId, answer.value])),
      );
      setStale(false);
      toast.success(t('schedulePoll.answerSaved', { count: updated.myAnswers.length }));
      synchronizeActions();
    } catch (reason) {
      if (staleError(reason)) {
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
    const initiallySelected = new Set(poll.members.map((member) => member.id));
    const requestedMemberIds = [...memberIdsDraft];
    setSavingMembers(true);
    try {
      const updated = await schedulePollApi.update(calendarId, pollId, {
        version: poll.version,
        memberIds: memberIdsDraft,
      });
      setPoll(updated);
      setAcknowledged(false);
      setMemberIdsDraft(updated.members.map((member) => member.id));
      setDraftAnswers(
        Object.fromEntries(updated.myAnswers.map((answer) => [answer.slotId, answer.value])),
      );
      toast.success(t('schedulePoll.membersUpdated'));
      synchronizeActions();
    } catch (reason) {
      if (staleError(reason)) {
        try {
          const latest = await reload();
          let currentRoster = roster;
          try {
            currentRoster = await schedulePollApi.members(calendarId);
            setRoster(currentRoster);
          } catch {
            // Keep the roster already on screen if it could not be refreshed.
          }
          const eligibleMemberIds = new Set(currentRoster.map((member) => member.id));
          const explicitlyRemoved = new Set(
            [...initiallySelected].filter((id) => !requestedMemberIds.includes(id)),
          );
          const mergedMemberIds = latest.members
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
      const updated = await schedulePollApi.update(calendarId, pollId, {
        version: poll.version,
        slots,
      });
      setPoll(updated);
      setAcknowledged(false);
      setKeptSlotIds(updated.slots.filter((slot) => slot.active).map((slot) => slot.id));
      setNewCandidates([]);
      toast.success(t('schedulePoll.candidatesUpdated'));
      synchronizeActions();
      navigateView('manage');
    } catch (reason) {
      if (staleError(reason)) {
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
      if (staleError(reason)) {
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
      const finalized = await schedulePollApi.finalize(calendarId, pollId, {
        version: poll.version,
        reviewVersion: poll.reviewVersion,
        slotId: selectedSlotId,
        acknowledged,
        ...(ownerId ? { ownerId } : {}),
      });
      setPoll(finalized);
      synchronizeActions();
      toast.success(t('schedulePoll.finalized'));
      if (finalized.confirmedEventId) navigateView('event');
    } catch (reason) {
      if (staleError(reason)) {
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
    if (!poll || !window.confirm(t('schedulePoll.cancelConfirm', { title: poll.title }))) return;
    setCancelling(true);
    try {
      const cancelled = await schedulePollApi.cancel(calendarId, pollId, poll.version);
      setPoll(cancelled);
      synchronizeActions();
      toast.success(t('schedulePoll.cancelled'));
    } catch (reason) {
      if (staleError(reason)) {
        try {
          await reload();
          setStale(true);
        } catch (reloadReason) {
          toast.error(errorMessage(reloadReason));
        }
      } else toast.error(errorMessage(reason));
    } finally {
      setCancelling(false);
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
  const allResponded = !!poll && poll.memberCount > 0 && poll.respondedCount >= poll.memberCount;
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

  if (loading) {
    return (
      <SchedulePollFrame title={t('schedulePoll.title')}>
        <p className="py-12 text-center text-body text-[var(--color-text-secondary)]">
          {t('common.loading')}
        </p>
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
          <section className="max-w-[760px] rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-5 sm:p-7">
            <div className="mb-4 flex flex-wrap items-center gap-2">
              <SchedulePollStatus status={poll.status} />
              <span className="text-footnote text-[var(--color-text-secondary)]">
                {t('schedulePoll.confirmedEvent')}
              </span>
            </div>
            <h2 className="text-title font-bold text-[var(--color-text-primary)]">{event.title}</h2>
            <p className="mt-2 text-body text-[var(--color-text-secondary)]">
              <SchedulePollSlotTime
                startAt={event.startAt}
                endAt={event.endAt}
                timezone={event.timezone ?? poll.timezone}
              />
            </p>
            {event.location && (
              <p className="mt-3 text-body text-[var(--color-text-primary)]">{event.location}</p>
            )}
            <div className="mt-5 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4">
              <h3 className="text-footnote font-semibold text-[var(--color-text-primary)]">
                {t('schedulePoll.visibilitySummary')}: {t(eventVisibilityKey)}
              </h3>
              <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
                {t(eventVisibilityHintKey)}
              </p>
            </div>
            <Link
              to="/schedule-polls/$calendarId/$pollId"
              params={{ calendarId, pollId }}
              search={{ view: poll.canManage ? 'manage' : 'respond' }}
              className="mt-5 inline-flex rounded-full bg-[var(--color-accent-bg)] px-4 py-2 text-body font-semibold text-[var(--color-accent)] hover:opacity-80"
            >
              {t('schedulePoll.backToPoll')}
            </Link>
          </section>
        ) : null}
      </SchedulePollFrame>
    );
  }

  const detailHeading = view === 'respond' ? t('schedulePoll.respond') : t('schedulePoll.manage');

  return (
    <SchedulePollFrame title={poll.title} eyebrow={t('schedulePoll.eyebrow')}>
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <SchedulePollStatus status={poll.status} />
        <span className="rounded-full bg-[var(--color-surface-inset)] px-2.5 py-1 text-footnote text-[var(--color-text-secondary)]">
          {t('schedulePoll.progress', {
            responded: poll.respondedCount,
            members: poll.memberCount,
          })}
        </span>
        {poll.canRespond && poll.status === 'open' && (
          <button
            type="button"
            onClick={() => navigateView('respond')}
            aria-current={view === 'respond' ? 'page' : undefined}
            className={`rounded-full px-3 py-1.5 text-footnote font-semibold ${view === 'respond' ? 'bg-[var(--color-accent)] text-white' : 'bg-[var(--color-surface-inset)] text-[var(--color-text-primary)]'}`}
          >
            {t('schedulePoll.respond')}
          </button>
        )}
        {poll.canManage && poll.status === 'open' && (
          <button
            type="button"
            onClick={() => navigateView('manage')}
            aria-current={view === 'manage' ? 'page' : undefined}
            className={`rounded-full px-3 py-1.5 text-footnote font-semibold ${view === 'manage' ? 'bg-[var(--color-accent)] text-white' : 'bg-[var(--color-surface-inset)] text-[var(--color-text-primary)]'}`}
          >
            {t('schedulePoll.manage')}
          </button>
        )}
        {poll.canTakeOver && poll.status === 'open' && (
          <button
            type="button"
            onClick={async () => {
              try {
                const updated = await schedulePollApi.update(calendarId, pollId, {
                  version: poll.version,
                  takeOver: true,
                });
                setPoll(updated);
                setAcknowledged(false);
                toast.success(t('schedulePoll.takenOver'));
                synchronizeActions();
              } catch (reason) {
                if (staleError(reason)) {
                  try {
                    await reload();
                    setStale(true);
                  } catch (reloadReason) {
                    toast.error(errorMessage(reloadReason));
                  }
                } else toast.error(errorMessage(reason));
              }
            }}
            className="rounded-full bg-[var(--color-accent-bg)] px-3 py-1.5 text-footnote font-semibold text-[var(--color-accent)]"
          >
            {t('schedulePoll.takeOver')}
          </button>
        )}
      </div>

      {refreshWarning && <SchedulePollNotice tone="warning">{refreshWarning}</SchedulePollNotice>}
      {stale && (
        <SchedulePollNotice tone="warning">{t('schedulePoll.staleReloaded')}</SchedulePollNotice>
      )}

      {view === 'respond' && (
        <section className="max-w-[900px]">
          <div className="mb-4">
            <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
              {detailHeading}
            </h2>
            <p className="mt-1 text-body text-[var(--color-text-secondary)]">
              {t('schedulePoll.answerInstruction')}
            </p>
          </div>
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
              <div className="space-y-3">
                {poll.slots
                  .filter((slot) => slot.active && slot.availability !== 'hard')
                  .map((slot) => (
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
              <div className="mt-5 flex flex-col gap-3 border-t border-[var(--color-separator)] pt-4 sm:flex-row sm:items-center sm:justify-between">
                <p
                  role="status"
                  className="text-footnote text-[var(--color-text-secondary)] tabular-nums"
                >
                  {t('schedulePoll.answerProgress', {
                    answered: answerCount,
                    total: activeSlots.length,
                  })}
                  {hasDraftChanges
                    ? ` · ${t('schedulePoll.unsaved')}`
                    : ` · ${t('schedulePoll.saved')}`}
                </p>
                <button
                  type="button"
                  onClick={saveResponse}
                  disabled={savingAnswers || !hasDraftChanges || activeSlots.length === 0}
                  className="btn-primary px-5 text-default disabled:opacity-50"
                >
                  {savingAnswers ? t('common.saving') : t('schedulePoll.saveAnswers')}
                </button>
              </div>
            </>
          )}
        </section>
      )}

      {view === 'manage' && (
        <section>
          {!poll.canManage ? (
            <SchedulePollNotice tone="error">{t('schedulePoll.noManageAccess')}</SchedulePollNotice>
          ) : (
            <>
              <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
                    {detailHeading}
                  </h2>
                  <p className="mt-1 text-body text-[var(--color-text-secondary)]">
                    {t('schedulePoll.managerProgress', {
                      responded: poll.respondedCount,
                      members: poll.memberCount,
                    })}
                  </p>
                </div>
                {poll.status === 'open' && (
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={() => navigateView('candidates')}
                      className="rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-footnote font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                    >
                      {t('schedulePoll.editCandidates')}
                    </button>
                    <button
                      type="button"
                      onClick={refreshAvailability}
                      className="rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-footnote font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                    >
                      {t('schedulePoll.refreshAvailability')}
                    </button>
                  </div>
                )}
              </div>

              {poll.status === 'open' && (
                <div className="mb-5 rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-5">
                  <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
                    <div>
                      <h3 className="text-default font-semibold text-[var(--color-text-primary)]">
                        {t('schedulePoll.invitees', { count: memberIdsDraft.length })}
                      </h3>
                      <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
                        {t('schedulePoll.inviteesHint')}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={saveMembers}
                      disabled={
                        savingMembers ||
                        memberIdsDraft.length === 0 ||
                        (memberIdsDraft.length === poll.members.length &&
                          poll.members.every((member) => memberIdsDraft.includes(member.id)))
                      }
                      className="rounded-full bg-[var(--color-accent-bg)] px-4 py-2 text-footnote font-semibold text-[var(--color-accent)] disabled:opacity-50"
                    >
                      {savingMembers ? t('common.saving') : t('schedulePoll.saveInvitees')}
                    </button>
                  </div>
                  <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {currentRoster.map((member) => (
                      <label
                        key={member.id}
                        className="flex min-h-10 items-center gap-3 rounded-xl bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)]"
                      >
                        <input
                          type="checkbox"
                          checked={memberIdsDraft.includes(member.id)}
                          onChange={(event) =>
                            setMemberIdsDraft((previous) =>
                              event.target.checked
                                ? [...new Set([...previous, member.id])]
                                : previous.filter((id) => id !== member.id),
                            )
                          }
                          className="h-4 w-4 accent-[var(--color-accent)]"
                        />
                        <span className="truncate">{member.name}</span>
                      </label>
                    ))}
                  </div>
                  {memberIdsDraft.length === 0 && (
                    <p className="mt-2 text-footnote text-[var(--color-danger)]">
                      {t('schedulePoll.selectAtLeastOne')}
                    </p>
                  )}
                </div>
              )}

              {poll.slots.length === 0 ? (
                <SchedulePollNotice tone="warning">
                  {t('schedulePoll.candidatesRevising')}
                </SchedulePollNotice>
              ) : (
                <>
                  <div className="hidden overflow-x-auto rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] md:block">
                    <table className="w-full border-collapse text-left">
                      <thead className="bg-[var(--color-surface-inset)]">
                        <tr>
                          <th
                            scope="col"
                            className="min-w-[240px] px-4 py-3 text-footnote font-semibold text-[var(--color-text-secondary)]"
                          >
                            {t('schedulePoll.candidate')}
                          </th>
                          <th
                            scope="col"
                            className="px-4 py-3 text-footnote font-semibold text-[var(--color-text-secondary)]"
                          >
                            {t('schedulePoll.availability')}
                          </th>
                          {poll.members.map((member) => (
                            <th
                              key={member.id}
                              scope="col"
                              className="min-w-[100px] px-3 py-3 text-center text-footnote font-semibold text-[var(--color-text-secondary)]"
                            >
                              {member.name}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[var(--color-separator)]">
                        {poll.slots.map((slot) => (
                          <tr key={slot.id} className={!slot.active ? 'opacity-55' : ''}>
                            <th
                              scope="row"
                              className="px-4 py-3 text-body font-medium text-[var(--color-text-primary)]"
                            >
                              <SchedulePollSlotTime
                                startAt={slot.startAt}
                                endAt={slot.endAt}
                                timezone={poll.timezone}
                              />
                            </th>
                            <td className="px-4 py-3">
                              <AvailabilityBadge availability={slot.availability} />
                              {!slot.active && (
                                <span className="ml-2 text-micro text-[var(--color-text-tertiary)]">
                                  {t('schedulePoll.unavailable')}
                                </span>
                              )}
                            </td>
                            {poll.members.map((member) => {
                              const value = responseFor(member.id, slot.id);
                              return (
                                <td
                                  key={member.id}
                                  className={`px-3 py-3 text-center text-body font-semibold ${answerTone(value)}`}
                                >
                                  {answerLabel(t, value)}
                                </td>
                              );
                            })}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>

                  <div className="space-y-2 md:hidden">
                    {poll.slots.map((slot) => (
                      <details
                        key={slot.id}
                        className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4"
                      >
                        <summary className="flex cursor-pointer list-none flex-wrap items-center justify-between gap-2 text-body font-semibold text-[var(--color-text-primary)]">
                          <SchedulePollSlotTime
                            startAt={slot.startAt}
                            endAt={slot.endAt}
                            timezone={poll.timezone}
                          />
                          <AvailabilityBadge availability={slot.availability} />
                        </summary>
                        <ul className="mt-3 divide-y divide-[var(--color-separator)]">
                          {poll.members.map((member) => {
                            const value = responseFor(member.id, slot.id);
                            return (
                              <li
                                key={member.id}
                                className="flex justify-between gap-3 py-2 text-footnote"
                              >
                                <span className="truncate text-[var(--color-text-secondary)]">
                                  {member.name}
                                </span>
                                <span className={`font-semibold ${answerTone(value)}`}>
                                  {answerLabel(t, value)}
                                </span>
                              </li>
                            );
                          })}
                        </ul>
                      </details>
                    ))}
                  </div>
                </>
              )}

              {hasNoUsableCandidates && (
                <div className="mt-4">
                  <SchedulePollNotice tone="warning">
                    {t('schedulePoll.addCandidatesRequired')}
                  </SchedulePollNotice>
                </div>
              )}

              {poll.status === 'open' && (
                <section className="mt-6 rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-5">
                  <h3 className="text-default font-semibold text-[var(--color-text-primary)]">
                    {t('schedulePoll.finalize')}
                  </h3>
                  {!allResponded && (
                    <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
                      {t('schedulePoll.waitingForResponses', {
                        count: poll.memberCount - poll.respondedCount,
                      })}
                    </p>
                  )}
                  {allResponded && activeSlots.length === 0 && (
                    <p className="mt-1 text-footnote text-[var(--color-warning,#e5a00d)]">
                      {t('schedulePoll.addCandidatesRequired')}
                    </p>
                  )}
                  <fieldset className="mt-3 space-y-2">
                    <legend className="sr-only">{t('schedulePoll.selectFinalCandidate')}</legend>
                    {poll.slots
                      .filter((slot) => slot.active && slot.availability !== 'hard')
                      .map((slot) => {
                        const responses =
                          poll.responses?.flatMap((response) =>
                            response.answers
                              .filter((answer) => answer.slotId === slot.id)
                              .map((answer) => answer.value),
                          ) ?? [];
                        const blocked = responses.includes('no');
                        const hasMaybe =
                          slot.availability === 'soft' || responses.includes('maybe');
                        return (
                          <label
                            key={slot.id}
                            className={`flex min-h-12 items-center gap-3 rounded-xl border px-3 py-2 text-body ${selectedSlotId === slot.id ? 'border-[color-mix(in_srgb,var(--color-accent)_45%,var(--color-border))] bg-[var(--color-accent-bg)]' : 'border-[var(--color-border)] bg-[var(--color-surface-inset)]'} ${blocked ? 'opacity-55' : ''}`}
                          >
                            <input
                              type="radio"
                              name="final-slot"
                              value={slot.id}
                              checked={selectedSlotId === slot.id}
                              disabled={!allResponded || blocked}
                              onChange={() => {
                                setSelectedSlotId(slot.id);
                                setAcknowledged(false);
                              }}
                              className="h-4 w-4 accent-[var(--color-accent)]"
                            />
                            <span className="min-w-0 flex-1 text-[var(--color-text-primary)]">
                              <SchedulePollSlotTime
                                startAt={slot.startAt}
                                endAt={slot.endAt}
                                timezone={poll.timezone}
                              />
                            </span>
                            <AvailabilityBadge availability={slot.availability} />
                            {blocked && (
                              <span className="text-micro font-semibold text-[var(--color-danger)]">
                                {t('schedulePoll.blockedByNo')}
                              </span>
                            )}
                            {!blocked && hasMaybe && (
                              <span className="text-micro text-[var(--color-warning,#e5a00d)]">
                                {t('schedulePoll.needsAcknowledgment')}
                              </span>
                            )}
                          </label>
                        );
                      })}
                  </fieldset>
                  <div className="mt-4 grid gap-3 sm:grid-cols-2">
                    <div className="block text-footnote font-semibold text-[var(--color-text-secondary)]">
                      <span className="mb-1.5 block">{t('schedulePoll.eventOwner')}</span>
                      <CustomSelect
                        value={ownerId}
                        onChange={setOwnerId}
                        ariaLabel={t('schedulePoll.eventOwner')}
                        className="w-full"
                        triggerClassName="input-modern"
                        options={poll.members.map((member) => ({
                          value: member.id,
                          label: member.name,
                        }))}
                      />
                    </div>
                    <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-3">
                      <p className="text-footnote font-semibold text-[var(--color-text-primary)]">
                        {t('schedulePoll.visibilitySummary')}
                      </p>
                      <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
                        {t('schedulePoll.confidentialVisibilityHint')}
                      </p>
                    </div>
                  </div>
                  {requiresAcknowledgment && selectedSlot && (
                    <label
                      className="mt-4 flex items-start gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-3 text-footnote text-[var(--color-text-primary)]"
                      style={{
                        borderColor:
                          'color-mix(in srgb, var(--color-warning, #e5a00d) 30%, var(--color-border))',
                        backgroundColor:
                          'color-mix(in srgb, var(--color-warning, #e5a00d) 10%, var(--color-surface))',
                      }}
                    >
                      <input
                        type="checkbox"
                        checked={acknowledged}
                        onChange={(event) => setAcknowledged(event.target.checked)}
                        className="mt-0.5 h-4 w-4 accent-[var(--color-accent)]"
                      />
                      <span>{t('schedulePoll.acknowledgeSoftSlot')}</span>
                    </label>
                  )}
                  {slotHasNo && selectedSlot && (
                    <p className="mt-3 text-footnote text-[var(--color-danger)]">
                      {t('schedulePoll.cannotFinalizeNo')}
                    </p>
                  )}
                  <button
                    type="button"
                    onClick={finalize}
                    disabled={
                      finalizing ||
                      !allResponded ||
                      !selectedSlotId ||
                      slotHasNo ||
                      (requiresAcknowledgment && !acknowledged)
                    }
                    className="btn-primary mt-4 px-5 text-default disabled:opacity-50"
                  >
                    {finalizing ? t('common.saving') : t('schedulePoll.finalizeAndCreateEvent')}
                  </button>
                </section>
              )}

              {poll.status === 'open' && (
                <div className="mt-5 border-t border-[var(--color-separator)] pt-4">
                  <button
                    type="button"
                    onClick={cancelPoll}
                    disabled={cancelling}
                    className="rounded-full px-3 py-2 text-footnote font-medium text-[var(--color-danger)] hover:bg-[var(--color-danger-bg)] disabled:opacity-50"
                  >
                    {cancelling ? t('common.saving') : t('schedulePoll.cancelPoll')}
                  </button>
                </div>
              )}
            </>
          )}
        </section>
      )}

      {view === 'candidates' && (
        <section>
          {!poll.canManage ? (
            <SchedulePollNotice tone="error">{t('schedulePoll.noManageAccess')}</SchedulePollNotice>
          ) : poll.status !== 'open' ? (
            <SchedulePollNotice>{t('schedulePoll.pollClosed')}</SchedulePollNotice>
          ) : (
            <>
              <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
                    {t('schedulePoll.editCandidates')}
                  </h2>
                  <p className="mt-1 text-body text-[var(--color-text-secondary)]">
                    {t('schedulePoll.candidateEditHint')}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => navigateView('manage')}
                  className="rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-footnote font-semibold text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                >
                  {t('schedulePoll.backToManage')}
                </button>
              </div>
              <div className="mb-4 rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-5">
                <h3 className="mb-3 text-default font-semibold text-[var(--color-text-primary)]">
                  {t('schedulePoll.currentCandidates')}
                </h3>
                {poll.slots.length === 0 ? (
                  <p className="text-footnote text-[var(--color-text-secondary)]">
                    {t('schedulePoll.noCandidates')}
                  </p>
                ) : (
                  <ul className="divide-y divide-[var(--color-separator)]">
                    {poll.slots.map((slot) => (
                      <li key={slot.id} className="flex min-h-12 items-center gap-3 py-2 text-body">
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
                          className="h-4 w-4 accent-[var(--color-accent)]"
                        />
                        <span className="min-w-0 flex-1 text-[var(--color-text-primary)]">
                          <SchedulePollSlotTime
                            startAt={slot.startAt}
                            endAt={slot.endAt}
                            timezone={poll.timezone}
                          />
                        </span>
                        <AvailabilityBadge availability={slot.availability} />
                        {!slot.active && (
                          <span className="text-micro text-[var(--color-text-tertiary)]">
                            {t('schedulePoll.unavailable')}
                          </span>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              <div className="mb-5">
                <SchedulePollCandidateFinder
                  calendarId={calendarId}
                  memberIds={memberIdsDraft}
                  selected={newCandidates}
                  onChange={setNewCandidates}
                />
              </div>
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--color-separator)] pt-4">
                <p className="text-footnote text-[var(--color-text-secondary)]">
                  {t('schedulePoll.candidateSaveReview', {
                    kept: keptSlotIds.length,
                    added: newCandidates.length,
                  })}
                </p>
                <button
                  type="button"
                  onClick={saveCandidates}
                  disabled={savingCandidates || keptSlotIds.length + newCandidates.length === 0}
                  className="btn-primary px-5 text-default disabled:opacity-50"
                >
                  {savingCandidates ? t('common.saving') : t('schedulePoll.saveCandidates')}
                </button>
              </div>
            </>
          )}
        </section>
      )}

      {poll.status === 'finalized' && (
        <div className="mt-6">
          {poll.confirmedEventId ? (
            <Link
              to="/schedule-polls/$calendarId/$pollId"
              params={{ calendarId, pollId }}
              search={{ view: 'event' }}
              className="inline-flex rounded-full bg-[var(--color-accent-bg)] px-4 py-2 text-body font-semibold text-[var(--color-accent)]"
            >
              {t('schedulePoll.viewEvent')}
            </Link>
          ) : (
            <SchedulePollNotice>{t('schedulePoll.eventUnavailable')}</SchedulePollNotice>
          )}
        </div>
      )}
    </SchedulePollFrame>
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
    <fieldset className="rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:px-5 sm:py-4">
      <legend className="sr-only">{t('schedulePoll.answerForCandidate')}</legend>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <p className="text-body font-semibold text-[var(--color-text-primary)]">
          <SchedulePollSlotTime startAt={slot.startAt} endAt={slot.endAt} timezone={timezone} />
        </p>
        <AvailabilityBadge availability={slot.availability} />
      </div>
      <div className="grid grid-cols-3 gap-2">
        {choices.map((choice) => (
          <label
            key={choice.value}
            className={`flex min-h-[68px] cursor-pointer flex-col items-center justify-center gap-0.5 rounded-xl border text-center transition focus-within:ring-2 focus-within:ring-[var(--color-accent)] focus-within:ring-offset-2 focus-within:ring-offset-[var(--color-surface)] ${value === choice.value ? 'border-[color-mix(in_srgb,var(--color-accent)_45%,var(--color-border))] bg-[var(--color-accent-bg)] text-[var(--color-accent)]' : 'border-[var(--color-border)] bg-[var(--color-surface-inset)] text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]'}`}
          >
            <input
              className="sr-only"
              type="radio"
              name={`answer-${slot.id}`}
              value={choice.value}
              checked={value === choice.value}
              onChange={() => onChange(choice.value)}
              aria-label={`${candidateLabel}: ${t(choice.mark)} ${t(choice.label)}`}
            />
            <span aria-hidden className="text-title font-bold leading-tight">
              {t(choice.mark)}
            </span>
            <span className="text-footnote font-semibold">{t(choice.label)}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
