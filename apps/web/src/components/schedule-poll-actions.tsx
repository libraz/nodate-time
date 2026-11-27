import { Link } from '@tanstack/react-router';
import { useT } from '@/i18n';
import type { PollActionKind, SchedulePollAction } from '@/lib/schedule-polls';
import { useAuthStore } from '@/stores/auth-store';
import { useSchedulePollStore } from '@/stores/schedule-poll-store';

const NO_ACTIONS: SchedulePollAction[] = [];

const ACTION_COPY: Record<
  PollActionKind,
  {
    label:
      | 'schedulePoll.statusAnswer'
      | 'schedulePoll.statusFinalize'
      | 'schedulePoll.statusAddSlots';
    action:
      | 'schedulePoll.actionAnswer'
      | 'schedulePoll.actionFinalize'
      | 'schedulePoll.actionAddSlots';
    view: 'respond' | 'manage' | 'candidates';
  }
> = {
  answer: {
    label: 'schedulePoll.statusAnswer',
    action: 'schedulePoll.actionAnswer',
    view: 'respond',
  },
  finalize: {
    label: 'schedulePoll.statusFinalize',
    action: 'schedulePoll.actionFinalize',
    view: 'manage',
  },
  'add-slots': {
    label: 'schedulePoll.statusAddSlots',
    action: 'schedulePoll.actionAddSlots',
    view: 'candidates',
  },
};

function actionTarget(action: SchedulePollAction) {
  return {
    to: '/schedule-polls/$calendarId/$pollId' as const,
    params: { calendarId: action.calendarId, pollId: action.pollId },
    search: { view: ACTION_COPY[action.kind].view },
  };
}

/** Calendar with a tick: the shared mark for the scheduling-poll entry points. */
function PollIcon() {
  return (
    <svg
      aria-hidden
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
    >
      <rect x="3.5" y="5" width="17" height="15.5" rx="2.5" />
      <path d="M3.5 10h17M8 3v4M16 3v4M9 15l2 2 4-4" />
    </svg>
  );
}

/** How many scheduling polls are waiting on the signed-in user. */
export function usePendingPollCount(): number {
  const userId = useAuthStore((state) => state.user?.id);
  return useSchedulePollStore((state) => (state.accountId === userId ? state.actions.length : 0));
}

/** The red count badge for pending poll actions; nothing when there are none. */
export function ActionCount({ count }: { count: number }) {
  if (count === 0) return null;
  return (
    <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-[var(--color-danger)] px-1.5 text-caption font-semibold leading-5 tabular-nums text-[var(--color-text-on-accent)]">
      {count > 99 ? '99+' : count}
    </span>
  );
}

export function SchedulePollNavDesktop() {
  const t = useT();
  const count = usePendingPollCount();
  return (
    <Link
      to="/schedule-polls"
      className="hidden h-9 items-center gap-1.5 rounded-lg px-2.5 text-default font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)] sm:inline-flex"
    >
      <span className="text-[var(--color-text-secondary)]">
        <PollIcon />
      </span>
      <span>{t('schedulePoll.nav')}</span>
      <ActionCount count={count} />
    </Link>
  );
}

export function SchedulePollActionNotice() {
  const t = useT();
  const userId = useAuthStore((state) => state.user?.id);
  const accountId = useSchedulePollStore((state) => state.accountId);
  const storedActions = useSchedulePollStore((state) => state.actions);
  const actions = accountId === userId ? storedActions : NO_ACTIONS;
  if (actions.length === 0) return null;

  const byKind = new Map<PollActionKind, SchedulePollAction[]>();
  for (const action of actions) {
    const current = byKind.get(action.kind) ?? [];
    current.push(action);
    byKind.set(action.kind, current);
  }

  return (
    <section
      aria-label={t('schedulePoll.pendingActions')}
      aria-live="polite"
      className="relative z-[1] hidden border-b border-[var(--color-border)] bg-[var(--color-accent-bg)] px-5 py-2 sm:block"
    >
      <div className="mx-auto flex max-w-[1600px] flex-col gap-1.5">
        {Array.from(byKind.entries()).map(([kind, group]) => {
          const first = group[0];
          if (!first) return null;
          const copy = ACTION_COPY[kind];
          return (
            <div
              key={kind}
              className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-footnote text-[var(--color-text-primary)]"
            >
              <span className="font-semibold">{t(copy.label)}</span>
              <span aria-hidden className="min-w-0 max-w-full truncate">
                {first.title}
              </span>
              <Link
                {...actionTarget(first)}
                aria-label={`${t(copy.action)} ${first.title}`}
                className="shrink-0 rounded-[var(--radius-control)] bg-[var(--color-surface)] px-2.5 py-1 font-semibold text-[var(--color-accent-ink)] ring-1 ring-[color-mix(in_srgb,var(--color-accent)_30%,var(--color-border))] hover:bg-[var(--color-hover)]"
              >
                {t(copy.action)}
              </Link>
              {group.length > 1 && (
                <Link
                  to="/schedule-polls"
                  className="shrink-0 font-medium text-[var(--color-accent-ink)] hover:underline"
                >
                  {t('schedulePoll.moreActions', { count: group.length - 1 })}
                </Link>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
