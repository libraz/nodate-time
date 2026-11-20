import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { type ReactNode, useEffect, useState } from 'react';
import { EmptyState, GUIDE_ICONS, TipCard } from '@/components/guide';
import { CustomSelect } from '@/components/pickers';
import { SchedulePollCandidateFinder } from '@/components/schedule-poll-candidate-finder';
import {
  InviteeGrid,
  SchedulePollFrame,
  SchedulePollNotice,
  SchedulePollProgress,
  SchedulePollSection,
  SchedulePollStatus,
} from '@/components/schedule-poll-ui';
import { Switch } from '@/components/switch';
import { useT } from '@/i18n';
import { errorMessage, isAbortError } from '@/lib/api';
import { canManage as roleCanManage } from '@/lib/permissions';
import {
  type SchedulePoll,
  type SchedulePollAction,
  type SchedulePollCandidate,
  schedulePollApi,
} from '@/lib/schedule-polls';
import { toast } from '@/lib/toast';
import { useCalendarMembers } from '@/lib/use-calendar-members';
import { useEnsureCalendars } from '@/lib/use-ensure-calendars';
import { useAuthStore } from '@/stores/auth-store';
import { useCalendarStore } from '@/stores/calendar-store';
import { useSchedulePollStore } from '@/stores/schedule-poll-store';
import { useUiStore } from '@/stores/ui-store';

export interface SchedulePollSearch {
  calendarId?: string | undefined;
}

const NO_ACTIONS: SchedulePollAction[] = [];

export const Route = createFileRoute('/schedule-polls')({
  validateSearch: (search: Record<string, unknown>): SchedulePollSearch => ({
    calendarId: typeof search.calendarId === 'string' ? search.calendarId : undefined,
  }),
  component: SchedulePollIndexPage,
});

function AvailabilitySharingControl({ calendarId }: { calendarId: string }) {
  const t = useT();
  const [enabled, setEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    schedulePollApi
      .getAvailabilitySharing(calendarId, controller.signal)
      .then(setEnabled)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted && !isAbortError(reason)) setError(errorMessage(reason));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [calendarId]);

  const change = async (next: boolean) => {
    setSaving(true);
    setError(null);
    try {
      setEnabled(await schedulePollApi.setAvailabilitySharing(calendarId, next));
    } catch (reason) {
      const message = errorMessage(reason);
      setError(message);
      toast.error(message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="page-card flex items-center justify-between gap-4 p-4 sm:px-5">
      <div className="min-w-0">
        <h2 className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('schedulePoll.availabilitySharing')}
        </h2>
        <p className="mt-0.5 max-w-[640px] text-footnote text-[var(--color-text-secondary)]">
          {t('schedulePoll.availabilitySharingHint')}
        </p>
        {error && (
          <p role="alert" className="mt-2 text-footnote text-[var(--color-danger)]">
            {error}
          </p>
        )}
      </div>
      <Switch
        label={t('schedulePoll.availabilitySharing')}
        checked={enabled}
        onChange={change}
        disabled={loading || saving || !!error}
        size="sm"
      />
    </section>
  );
}

function SchedulePollIndexPage() {
  const t = useT();
  const navigate = useNavigate();
  const search = Route.useSearch();
  const isAuthenticated = useAuthStore((state) => state.isAuthenticated);
  const calendars = useCalendarStore((state) => state.calendars);
  const timezone = useUiStore((state) => state.timezone);
  const userId = useAuthStore((state) => state.user?.id);
  const refreshActions = useSchedulePollStore((state) => state.fetchActions);
  const actionAccountId = useSchedulePollStore((state) => state.accountId);
  const storedActions = useSchedulePollStore((state) => state.actions);
  const actions = actionAccountId === userId ? storedActions : NO_ACTIONS;
  const [selectedCalendarId, setSelectedCalendarId] = useState(search.calendarId ?? '');
  const [polls, setPolls] = useState<SchedulePoll[]>([]);
  const [pollsLoading, setPollsLoading] = useState(false);
  const [pollsError, setPollsError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [title, setTitle] = useState('');
  const [candidateSlots, setCandidateSlots] = useState<SchedulePollCandidate[]>([]);
  const [saving, setSaving] = useState(false);

  useEnsureCalendars(isAuthenticated);

  useEffect(() => {
    if (search.calendarId && calendars.some((calendar) => calendar.id === search.calendarId)) {
      setSelectedCalendarId(search.calendarId);
      return;
    }
    if (!calendars.some((calendar) => calendar.id === selectedCalendarId)) {
      setSelectedCalendarId(calendars[0]?.id ?? '');
    }
  }, [calendars, search.calendarId, selectedCalendarId]);

  const selectedCalendar = calendars.find((calendar) => calendar.id === selectedCalendarId);
  const canManage = roleCanManage(selectedCalendar?.role);

  useEffect(() => {
    if (!selectedCalendarId) {
      setPolls([]);
      return;
    }
    const controller = new AbortController();
    setPollsLoading(true);
    setPollsError(null);
    schedulePollApi
      .list(selectedCalendarId, controller.signal)
      .then(setPolls)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) setPollsError(errorMessage(reason));
      })
      .finally(() => {
        if (!controller.signal.aborted) setPollsLoading(false);
      });
    return () => controller.abort();
  }, [selectedCalendarId]);

  const pickerCalendarId = creating && canManage ? selectedCalendarId : '';
  const { members: calendarMembers } = useCalendarMembers(pickerCalendarId);
  const members = [...calendarMembers].sort((a, b) => a.name.localeCompare(b.name));
  const membersLoaded = useCalendarStore((state) => pickerCalendarId in state.membersMap);
  const membersError = useCalendarStore((state) => state.memberErrors[pickerCalendarId] ?? null);
  const membersLoading = !!pickerCalendarId && !membersLoaded && !membersError;
  const memberKey = members.map((member) => member.id).join(',');

  useEffect(() => {
    setMemberIds(memberKey ? memberKey.split(',') : []);
  }, [memberKey]);

  useEffect(() => {
    if (userId) void refreshActions(userId);
  }, [refreshActions, userId]);

  const openCalendar = (calendarId: string) => {
    setSelectedCalendarId(calendarId);
    setCreating(false);
    setCandidateSlots([]);
    setTitle('');
    void navigate({ to: '/schedule-polls', search: { calendarId } });
  };

  const setInvitee = (memberId: string, checked: boolean) => {
    setMemberIds((previous) =>
      checked
        ? previous.includes(memberId)
          ? previous
          : [...previous, memberId]
        : previous.filter((id) => id !== memberId),
    );
    setCandidateSlots([]);
  };

  const createPoll = async () => {
    if (
      !selectedCalendarId ||
      !title.trim() ||
      memberIds.length === 0 ||
      candidateSlots.length === 0
    )
      return;
    setSaving(true);
    try {
      const created = await schedulePollApi.create(selectedCalendarId, {
        title: title.trim(),
        timezone,
        memberIds,
        slots: candidateSlots.map(({ startAt, endAt }) => ({ startAt, endAt })),
      });
      void refreshActions(userId ?? '');
      toast.success(t('schedulePoll.created'));
      await navigate({
        to: '/schedule-polls/$calendarId/$pollId',
        params: { calendarId: selectedCalendarId, pollId: created.id },
        search: { view: 'manage' },
      });
    } catch (reason) {
      toast.error(errorMessage(reason));
    } finally {
      setSaving(false);
    }
  };

  const toggleCreate = () => {
    setCreating((current) => !current);
    setCandidateSlots([]);
    setTitle('');
  };

  const actionGroups = new Map<string, typeof actions>();
  for (const action of actions) {
    const current = actionGroups.get(action.kind) ?? [];
    current.push(action);
    actionGroups.set(action.kind, current);
  }
  const orderedActions = Array.from(actionGroups.values()).flat();

  return (
    <SchedulePollFrame
      title={t('schedulePoll.title')}
      eyebrow={t('schedulePoll.eyebrow')}
      backTo="/"
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0 flex-1 text-footnote font-medium text-[var(--color-text-secondary)]">
          <span className="mb-1.5 block">{t('schedulePoll.calendar')}</span>
          <CustomSelect
            value={selectedCalendarId}
            onChange={openCalendar}
            ariaLabel={t('schedulePoll.calendar')}
            className="w-full sm:max-w-[360px]"
            triggerClassName="input-modern"
            options={calendars.map((calendar) => ({ value: calendar.id, label: calendar.name }))}
          />
        </div>
        {canManage && selectedCalendarId && (
          <button
            type="button"
            onClick={toggleCreate}
            aria-expanded={creating}
            className={`${creating ? 'btn-secondary' : 'btn-primary'} inline-flex items-center justify-center gap-2 px-5 text-default`}
          >
            {!creating && <PlusIcon />}
            {creating ? t('schedulePoll.cancelCreate') : t('schedulePoll.create')}
          </button>
        )}
      </div>

      {creating && canManage && (
        <div className="flex flex-col gap-5">
          <p className="text-body text-[var(--color-text-secondary)]">
            {t('schedulePoll.createHint')}
          </p>
          {membersError && <SchedulePollNotice tone="error">{membersError}</SchedulePollNotice>}
          {membersLoading ? (
            <p className="py-5 text-body text-[var(--color-text-secondary)]">
              {t('common.loading')}
            </p>
          ) : members.length === 0 ? (
            <SchedulePollNotice>{t('schedulePoll.noMembers')}</SchedulePollNotice>
          ) : (
            <>
              <CreateStep index={1} title={t('schedulePoll.invitees', { count: memberIds.length })}>
                <fieldset className="page-card p-4 sm:p-5">
                  <legend className="sr-only">
                    {t('schedulePoll.invitees', { count: memberIds.length })}
                  </legend>
                  <p className="mb-3 text-footnote text-[var(--color-text-secondary)]">
                    {t('schedulePoll.inviteesHint')}
                  </p>
                  <InviteeGrid members={members} selectedIds={memberIds} onToggle={setInvitee} />
                  {memberIds.length === 0 && (
                    <p role="alert" className="mt-3 text-footnote text-[var(--color-danger)]">
                      {t('schedulePoll.selectAtLeastOne')}
                    </p>
                  )}
                </fieldset>
              </CreateStep>

              <CreateStep
                index={2}
                title={t('schedulePoll.candidate')}
                muted={memberIds.length === 0}
              >
                {memberIds.length > 0 && (
                  <SchedulePollCandidateFinder
                    calendarId={selectedCalendarId}
                    memberIds={memberIds}
                    selected={candidateSlots}
                    onChange={setCandidateSlots}
                  />
                )}
              </CreateStep>

              <CreateStep
                index={3}
                title={t('schedulePoll.pollName')}
                muted={memberIds.length === 0}
              >
                {memberIds.length > 0 && (
                  <div className="page-card p-4 sm:p-5">
                    <input
                      type="text"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      maxLength={200}
                      aria-label={t('schedulePoll.pollName')}
                      placeholder={t('schedulePoll.pollNamePlaceholder')}
                      className="input-modern w-full"
                    />
                    <div className="mt-4 flex flex-col items-stretch justify-between gap-3 border-t border-[var(--color-separator)] pt-4 sm:flex-row sm:items-center">
                      <p className="text-footnote text-[var(--color-text-secondary)] tabular-nums">
                        {t('schedulePoll.createReview', {
                          calendar: selectedCalendar?.name ?? '',
                          members: memberIds.length,
                          candidates: candidateSlots.length,
                        })}
                      </p>
                      <button
                        type="button"
                        onClick={createPoll}
                        disabled={
                          saving ||
                          !title.trim() ||
                          memberIds.length === 0 ||
                          candidateSlots.length === 0
                        }
                        className="btn-primary shrink-0 px-6 text-default"
                      >
                        {saving ? t('common.saving') : t('schedulePoll.sendRequest')}
                      </button>
                    </div>
                  </div>
                )}
              </CreateStep>
            </>
          )}
        </div>
      )}

      {orderedActions.length > 0 && (
        <SchedulePollSection title={t('schedulePoll.myActions')}>
          <ul className="page-card divide-y divide-[var(--color-separator)] overflow-hidden">
            {orderedActions.map((action) => (
              <li key={`${action.kind}:${action.calendarId}:${action.pollId}`}>
                <Link
                  to="/schedule-polls/$calendarId/$pollId"
                  params={{ calendarId: action.calendarId, pollId: action.pollId }}
                  search={{
                    view:
                      action.kind === 'answer'
                        ? 'respond'
                        : action.kind === 'finalize'
                          ? 'manage'
                          : 'candidates',
                  }}
                  className="group flex items-center gap-3 px-4 py-3 transition hover:bg-[var(--color-hover)] sm:px-5"
                >
                  <span
                    aria-hidden
                    className="h-2 w-2 shrink-0 rounded-full bg-[var(--color-accent)]"
                  />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-default font-semibold text-[var(--color-text-primary)]">
                      {action.title}
                    </span>
                    <span className="mt-0.5 block truncate text-footnote text-[var(--color-text-secondary)]">
                      {t(
                        action.kind === 'answer'
                          ? 'schedulePoll.statusAnswer'
                          : action.kind === 'finalize'
                            ? 'schedulePoll.statusFinalize'
                            : 'schedulePoll.statusAddSlots',
                      )}
                      {' · '}
                      {action.calendarName}
                    </span>
                  </span>
                  <span className="shrink-0 rounded-full bg-[var(--color-accent-bg)] px-3 py-1.5 text-footnote font-semibold text-[var(--color-accent-ink)] transition group-hover:bg-[var(--color-accent-subtle)]">
                    {t(
                      action.kind === 'answer'
                        ? 'schedulePoll.actionAnswer'
                        : action.kind === 'finalize'
                          ? 'schedulePoll.actionFinalize'
                          : 'schedulePoll.actionAddSlots',
                    )}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </SchedulePollSection>
      )}

      <SchedulePollSection
        title={t('schedulePoll.calendarPolls')}
        hint={selectedCalendar?.name ?? t('schedulePoll.selectCalendar')}
        actions={
          pollsLoading ? (
            <span aria-hidden className="spinner h-4 w-4 text-[var(--color-text-tertiary)]" />
          ) : undefined
        }
      >
        <TipCard id="polls" topic="polls" className="mb-4">
          {t('tip.polls')}
        </TipCard>
        {pollsError && <SchedulePollNotice tone="error">{pollsError}</SchedulePollNotice>}
        {!selectedCalendarId ? (
          <SchedulePollNotice>{t('schedulePoll.noCalendars')}</SchedulePollNotice>
        ) : polls.length === 0 && !pollsLoading ? (
          <div className="rounded-[var(--radius-lg)] border border-dashed border-[var(--color-border-strong)]">
            <EmptyState
              icon={GUIDE_ICONS.poll}
              title={t('empty.pollsTitle')}
              description={t('empty.pollsDesc')}
            />
          </div>
        ) : (
          <ul className="page-card divide-y divide-[var(--color-separator)] overflow-hidden">
            {polls.map((poll) => (
              <li key={poll.id} className="flex items-stretch">
                <Link
                  to="/schedule-polls/$calendarId/$pollId"
                  params={{ calendarId: poll.calendarId, pollId: poll.id }}
                  search={{ view: poll.canManage || poll.canTakeOver ? 'manage' : 'respond' }}
                  aria-label={`${poll.title} — ${
                    poll.status === 'open'
                      ? t('schedulePoll.openPoll')
                      : t('schedulePoll.viewResult')
                  }`}
                  className="group flex min-w-0 flex-1 items-center gap-4 px-4 py-3.5 transition hover:bg-[var(--color-hover)] sm:px-5"
                >
                  <span className="min-w-0 flex-1">
                    <span className="flex min-w-0 items-center gap-2.5">
                      <span
                        className={`truncate text-default font-semibold ${poll.status === 'cancelled' ? 'text-[var(--color-text-secondary)]' : 'text-[var(--color-text-primary)]'}`}
                      >
                        {poll.title}
                      </span>
                      <SchedulePollStatus status={poll.status} />
                    </span>
                    <SchedulePollProgress
                      responded={poll.respondedCount}
                      members={poll.memberCount}
                      className="mt-1.5"
                    />
                  </span>
                  <ChevronIcon />
                </Link>
                {poll.confirmedEventId && (
                  <Link
                    to="/schedule-polls/$calendarId/$pollId"
                    params={{ calendarId: poll.calendarId, pollId: poll.id }}
                    search={{ view: 'event' }}
                    className="flex shrink-0 items-center border-l border-[var(--color-separator)] px-4 text-footnote font-semibold text-[var(--color-accent-ink)] transition hover:bg-[var(--color-hover)]"
                  >
                    {t('schedulePoll.viewEvent')}
                  </Link>
                )}
              </li>
            ))}
          </ul>
        )}
      </SchedulePollSection>

      {canManage && selectedCalendarId && (
        <AvailabilitySharingControl key={selectedCalendarId} calendarId={selectedCalendarId} />
      )}
    </SchedulePollFrame>
  );
}

function CreateStep({
  index,
  title,
  muted = false,
  children,
}: {
  index: number;
  title: string;
  muted?: boolean;
  children: ReactNode;
}) {
  return (
    <section className={muted ? 'opacity-50' : undefined}>
      <h2 className="mb-2.5 flex items-center gap-2.5 text-default font-semibold text-[var(--color-text-primary)]">
        <span
          aria-hidden
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-accent)] text-footnote font-bold text-[var(--color-text-on-accent)] tabular-nums"
        >
          {index}
        </span>
        {title}
      </h2>
      {children}
    </section>
  );
}

function PlusIcon() {
  return (
    <svg
      aria-hidden
      width="16"
      height="16"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
    >
      <path d="M8 3v10M3 8h10" />
    </svg>
  );
}

function ChevronIcon() {
  return (
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
      className="shrink-0 text-[var(--color-text-tertiary)] transition group-hover:translate-x-0.5 group-hover:text-[var(--color-text-secondary)]"
    >
      <path d="m5 2.5 4.5 4.5L5 11.5" />
    </svg>
  );
}
