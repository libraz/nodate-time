import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { CustomSelect } from '@/components/pickers';
import { SchedulePollCandidateFinder } from '@/components/schedule-poll-candidate-finder';
import {
  SchedulePollFrame,
  SchedulePollNotice,
  SchedulePollStatus,
} from '@/components/schedule-poll-ui';
import { Switch } from '@/components/switch';
import { useT } from '@/i18n';
import { errorMessage, isAbortError } from '@/lib/api';
import { canManage as roleCanManage } from '@/lib/permissions';
import {
  type PollMemberListItem,
  type SchedulePoll,
  type SchedulePollAction,
  type SchedulePollCandidate,
  schedulePollApi,
} from '@/lib/schedule-polls';
import { toast } from '@/lib/toast';
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
    <section className="mb-5 flex flex-col gap-3 rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:flex-row sm:items-center sm:justify-between sm:px-5">
      <div className="min-w-0">
        <h2 className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('schedulePoll.availabilitySharing')}
        </h2>
        <p className="mt-1 max-w-[680px] text-footnote text-[var(--color-text-secondary)]">
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
  const [members, setMembers] = useState<PollMemberListItem[]>([]);
  const [membersLoading, setMembersLoading] = useState(false);
  const [membersError, setMembersError] = useState<string | null>(null);
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

  useEffect(() => {
    if (!creating || !canManage || !selectedCalendarId) {
      setMembers([]);
      setMemberIds([]);
      setMembersError(null);
      return;
    }
    const controller = new AbortController();
    setMembersLoading(true);
    setMembersError(null);
    schedulePollApi
      .members(selectedCalendarId, controller.signal)
      .then((result) => {
        setMembers(result);
        setMemberIds(result.map((member) => member.id));
      })
      .catch((reason: unknown) => {
        if (!controller.signal.aborted) setMembersError(errorMessage(reason));
      })
      .finally(() => {
        if (!controller.signal.aborted) setMembersLoading(false);
      });
    return () => controller.abort();
  }, [canManage, creating, selectedCalendarId]);

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

  return (
    <SchedulePollFrame
      title={t('schedulePoll.title')}
      eyebrow={t('schedulePoll.eyebrow')}
      backTo="/"
    >
      <div className="mb-5 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="block min-w-0 flex-1 text-footnote font-semibold text-[var(--color-text-secondary)]">
          <span className="mb-1.5 block">{t('schedulePoll.calendar')}</span>
          <CustomSelect
            value={selectedCalendarId}
            onChange={openCalendar}
            ariaLabel={t('schedulePoll.calendar')}
            className="w-full max-w-[520px]"
            triggerClassName="input-modern"
            options={calendars.map((calendar) => ({ value: calendar.id, label: calendar.name }))}
          />
        </div>
        {canManage && selectedCalendarId && (
          <button type="button" onClick={toggleCreate} className="btn-primary px-5 text-default">
            {creating ? t('schedulePoll.cancelCreate') : t('schedulePoll.create')}
          </button>
        )}
      </div>

      {actions.length > 0 && (
        <section className="mb-6">
          <h2 className="mb-3 text-title font-semibold text-[var(--color-text-primary)]">
            {t('schedulePoll.myActions')}
          </h2>
          <div className="grid gap-3 md:grid-cols-2">
            {Array.from(actionGroups.entries()).flatMap(([kind, group]) =>
              group.map((action) => (
                <Link
                  key={`${kind}:${action.calendarId}:${action.pollId}`}
                  to="/schedule-polls/$calendarId/$pollId"
                  params={{ calendarId: action.calendarId, pollId: action.pollId }}
                  search={{
                    view:
                      kind === 'answer' ? 'respond' : kind === 'finalize' ? 'manage' : 'candidates',
                  }}
                  className="flex items-center justify-between gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 text-left transition hover:bg-[var(--color-hover)]"
                >
                  <span className="min-w-0">
                    <span className="block truncate text-footnote font-semibold text-[var(--color-accent)]">
                      {t(
                        kind === 'answer'
                          ? 'schedulePoll.statusAnswer'
                          : kind === 'finalize'
                            ? 'schedulePoll.statusFinalize'
                            : 'schedulePoll.statusAddSlots',
                      )}
                    </span>
                    <span className="mt-0.5 block truncate text-default font-semibold text-[var(--color-text-primary)]">
                      {action.title}
                    </span>
                    <span className="mt-0.5 block truncate text-footnote text-[var(--color-text-secondary)]">
                      {action.calendarName}
                    </span>
                  </span>
                  <span className="shrink-0 rounded-full bg-[var(--color-accent-bg)] px-3 py-1.5 text-footnote font-semibold text-[var(--color-accent)]">
                    {t(
                      kind === 'answer'
                        ? 'schedulePoll.actionAnswer'
                        : kind === 'finalize'
                          ? 'schedulePoll.actionFinalize'
                          : 'schedulePoll.actionAddSlots',
                    )}
                  </span>
                </Link>
              )),
            )}
          </div>
        </section>
      )}

      {canManage && selectedCalendarId && (
        <AvailabilitySharingControl key={selectedCalendarId} calendarId={selectedCalendarId} />
      )}

      {creating && canManage && (
        <section className="mb-6 rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)] p-4 sm:p-6">
          <div className="mb-5">
            <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
              {t('schedulePoll.create')}
            </h2>
            <p className="mt-1 text-body text-[var(--color-text-secondary)]">
              {t('schedulePoll.createHint')}
            </p>
          </div>

          {membersError && <SchedulePollNotice tone="error">{membersError}</SchedulePollNotice>}
          {membersLoading ? (
            <p className="py-5 text-body text-[var(--color-text-secondary)]">
              {t('common.loading')}
            </p>
          ) : members.length === 0 ? (
            <SchedulePollNotice>{t('schedulePoll.noMembers')}</SchedulePollNotice>
          ) : (
            <>
              <fieldset className="mb-5">
                <legend className="mb-2 text-default font-semibold text-[var(--color-text-primary)]">
                  {t('schedulePoll.invitees', { count: memberIds.length })}
                </legend>
                <p className="mb-3 text-footnote text-[var(--color-text-secondary)]">
                  {t('schedulePoll.inviteesHint')}
                </p>
                <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                  {members.map((member) => (
                    <label
                      key={member.id}
                      className="flex min-h-11 items-center gap-3 rounded-xl border border-[var(--color-border)] bg-[var(--color-surface-inset)] px-3 py-2 text-body text-[var(--color-text-primary)]"
                    >
                      <input
                        type="checkbox"
                        checked={memberIds.includes(member.id)}
                        onChange={(event) => setInvitee(member.id, event.target.checked)}
                        className="h-4 w-4 accent-[var(--color-accent)]"
                      />
                      <span className="truncate">{member.name}</span>
                    </label>
                  ))}
                </div>
              </fieldset>

              {memberIds.length === 0 && (
                <SchedulePollNotice tone="warning">
                  {t('schedulePoll.selectAtLeastOne')}
                </SchedulePollNotice>
              )}
              {memberIds.length > 0 && (
                <>
                  <div className="mb-5">
                    <SchedulePollCandidateFinder
                      calendarId={selectedCalendarId}
                      memberIds={memberIds}
                      selected={candidateSlots}
                      onChange={setCandidateSlots}
                    />
                  </div>
                  <label className="mb-5 block max-w-[640px] text-footnote font-semibold text-[var(--color-text-secondary)]">
                    <span className="mb-1.5 block">{t('schedulePoll.pollName')}</span>
                    <input
                      type="text"
                      value={title}
                      onChange={(event) => setTitle(event.target.value)}
                      maxLength={200}
                      placeholder={t('schedulePoll.pollNamePlaceholder')}
                      className="input-modern w-full"
                    />
                  </label>
                  <div className="flex flex-col items-start justify-between gap-3 border-t border-[var(--color-separator)] pt-4 sm:flex-row sm:items-center">
                    <p className="text-footnote text-[var(--color-text-secondary)]">
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
                      className="btn-primary px-5 text-default disabled:opacity-50"
                    >
                      {saving ? t('common.saving') : t('schedulePoll.sendRequest')}
                    </button>
                  </div>
                </>
              )}
            </>
          )}
        </section>
      )}

      <section>
        <div className="mb-3 flex items-end justify-between gap-3">
          <div>
            <h2 className="text-title font-semibold text-[var(--color-text-primary)]">
              {t('schedulePoll.calendarPolls')}
            </h2>
            <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
              {selectedCalendar?.name ?? t('schedulePoll.selectCalendar')}
            </p>
          </div>
          {pollsLoading && (
            <span className="text-footnote text-[var(--color-text-secondary)]">
              {t('common.loading')}
            </span>
          )}
        </div>

        {pollsError && <SchedulePollNotice tone="error">{pollsError}</SchedulePollNotice>}
        {!selectedCalendarId ? (
          <SchedulePollNotice>{t('schedulePoll.noCalendars')}</SchedulePollNotice>
        ) : polls.length === 0 && !pollsLoading ? (
          <div className="rounded-2xl border border-dashed border-[var(--color-border)] px-5 py-10 text-center text-body text-[var(--color-text-secondary)]">
            {t('schedulePoll.noPolls')}
          </div>
        ) : (
          <div className="divide-y divide-[var(--color-separator)] overflow-hidden rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface)]">
            {polls.map((poll) => (
              <div
                key={poll.id}
                className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between sm:px-5"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="truncate text-default font-semibold text-[var(--color-text-primary)]">
                      {poll.title}
                    </h3>
                    <SchedulePollStatus status={poll.status} />
                  </div>
                  <p className="mt-1 text-footnote text-[var(--color-text-secondary)] tabular-nums">
                    {poll.respondedCount} / {poll.memberCount} · {t('schedulePoll.responded')}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  {poll.confirmedEventId && (
                    <Link
                      to="/schedule-polls/$calendarId/$pollId"
                      params={{ calendarId: poll.calendarId, pollId: poll.id }}
                      search={{ view: 'event' }}
                      className="rounded-full bg-[var(--color-surface-inset)] px-3 py-1.5 text-footnote font-medium text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]"
                    >
                      {t('schedulePoll.viewEvent')}
                    </Link>
                  )}
                  <Link
                    to="/schedule-polls/$calendarId/$pollId"
                    params={{ calendarId: poll.calendarId, pollId: poll.id }}
                    search={{ view: poll.canManage || poll.canTakeOver ? 'manage' : 'respond' }}
                    className="rounded-full bg-[var(--color-accent-bg)] px-3.5 py-1.5 text-footnote font-semibold text-[var(--color-accent)] hover:opacity-80"
                  >
                    {poll.status === 'open'
                      ? t('schedulePoll.openPoll')
                      : t('schedulePoll.viewResult')}
                  </Link>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </SchedulePollFrame>
  );
}
