import { Link } from '@tanstack/react-router';
import { DateTime } from 'luxon';
import type { ReactNode } from 'react';
import { Avatar } from '@/components/avatar';
import { BrandRow } from '@/components/brand-mark';
import { useT } from '@/i18n';
import type {
  PollAnswer,
  PollAvailability,
  PollMemberListItem,
  SchedulePoll,
} from '@/lib/schedule-polls';
import { useUiStore } from '@/stores/ui-store';

export function SchedulePollFrame({
  title,
  eyebrow,
  children,
  backTo = '/schedule-polls',
}: {
  title: string;
  eyebrow?: string;
  children: ReactNode;
  backTo?: '/' | '/schedule-polls';
}) {
  const t = useT();
  return (
    <div className="app-bg min-h-screen">
      <div className="mx-auto flex min-h-screen max-w-[1040px] flex-col px-4 py-5 sm:px-6 sm:py-8">
        <header className="mb-6 flex items-start justify-between gap-4 sm:mb-8">
          <div className="min-w-0">
            <div className="mb-4">
              <BrandRow />
            </div>
            {eyebrow && (
              <p className="mb-1 text-footnote font-medium text-[var(--color-text-secondary)]">
                {eyebrow}
              </p>
            )}
            <h1 className="break-words text-display font-bold text-[var(--color-text-primary)] sm:text-hero">
              {title}
            </h1>
          </div>
          <Link
            to={backTo}
            className="shrink-0 rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-body font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)]"
          >
            {backTo === '/' ? t('schedulePoll.backToCalendar') : t('schedulePoll.backToList')}
          </Link>
        </header>
        <main className="flex flex-1 flex-col gap-6 pb-10 sm:gap-8">{children}</main>
      </div>
    </div>
  );
}

/** Section title with an optional hint and right-aligned actions. */
export function SchedulePollSection({
  title,
  hint,
  actions,
  children,
  className = '',
}: {
  title: ReactNode;
  hint?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={className}>
      <div className="mb-3 flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="min-w-0">
          <h2 className="text-title font-semibold text-[var(--color-text-primary)]">{title}</h2>
          {hint && (
            <p className="mt-0.5 text-footnote text-[var(--color-text-secondary)]">{hint}</p>
          )}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

export function SchedulePollStatus({ status }: { status: SchedulePoll['status'] }) {
  const t = useT();
  const copy =
    status === 'open'
      ? t('schedulePoll.statusOpen')
      : status === 'finalized'
        ? t('schedulePoll.statusFinalized')
        : t('schedulePoll.statusCancelled');
  const tone =
    status === 'open'
      ? 'var(--color-accent)'
      : status === 'finalized'
        ? 'var(--color-success)'
        : 'var(--color-text-tertiary)';
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 text-footnote font-medium text-[var(--color-text-secondary)]">
      <span aria-hidden className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: tone }} />
      {copy}
    </span>
  );
}

/** Responded / invited as a short meter with its count. */
export function SchedulePollProgress({
  responded,
  members,
  className = '',
}: {
  responded: number;
  members: number;
  className?: string;
}) {
  const t = useT();
  const ratio = members > 0 ? Math.min(1, responded / members) : 0;
  return (
    <span className={`flex items-center gap-2.5 ${className}`}>
      <span className="progress-track w-20 shrink-0" aria-hidden>
        <span className="progress-fill block" style={{ width: `${ratio * 100}%` }} />
      </span>
      <span className="text-footnote text-[var(--color-text-secondary)] tabular-nums">
        {t('schedulePoll.progress', { responded, members })}
      </span>
    </span>
  );
}

export function AvailabilityBadge({ availability }: { availability: PollAvailability }) {
  const t = useT();
  const tone =
    availability === 'open'
      ? 'var(--color-success)'
      : availability === 'soft'
        ? 'var(--color-warning)'
        : 'var(--color-danger)';
  const label =
    availability === 'open'
      ? t('schedulePoll.availabilityOpen')
      : availability === 'soft'
        ? t('schedulePoll.availabilitySoft')
        : t('schedulePoll.availabilityHard');
  return (
    <span
      className="inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-micro font-semibold"
      style={{
        borderColor: `color-mix(in srgb, ${tone} 30%, transparent)`,
        backgroundColor: `color-mix(in srgb, ${tone} 10%, transparent)`,
        color: `color-mix(in srgb, ${tone} 72%, var(--color-text-primary))`,
      }}
    >
      {label}
    </span>
  );
}

/** ○ / △ / × drawn as strokes so every theme's font renders the same shape. */
export function AnswerMark({
  answer,
  size = 16,
  className = '',
}: {
  answer: PollAnswer | undefined;
  size?: number;
  className?: string;
}) {
  return (
    <svg
      aria-hidden
      data-answer={answer ?? 'none'}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={answer ? 2 : 1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`answer-mark shrink-0 ${className}`}
    >
      {answer === 'yes' && <circle cx="8" cy="8" r="5.5" />}
      {answer === 'maybe' && <path d="M8 2.75 13.5 12.75h-11z" />}
      {answer === 'no' && <path d="m3.5 3.5 9 9m0-9-9 9" />}
      {!answer && <path d="M5 8h6" />}
    </svg>
  );
}

export function formatSchedulePollSlotTime(
  startAt: string,
  endAt: string,
  timezone: string,
  locale: string,
): string {
  const start = DateTime.fromISO(startAt, { setZone: true }).setZone(timezone).setLocale(locale);
  const end = DateTime.fromISO(endAt, { setZone: true }).setZone(timezone).setLocale(locale);
  const sameDate = start.hasSame(end, 'day');
  const endText = end.toLocaleString(
    sameDate ? DateTime.TIME_SIMPLE : DateTime.DATETIME_MED_WITH_WEEKDAY,
  );
  const offsetNote =
    start.offset === end.offset ? '' : ` (UTC${start.toFormat('ZZ')} – UTC${end.toFormat('ZZ')})`;
  return `${start.toLocaleString(DateTime.DATETIME_MED_WITH_WEEKDAY)} – ${endText}${offsetNote}`;
}

/** The time half of a candidate: a plain range on one day, the end date when it crosses one. */
export function slotTimeRange(start: DateTime, end: DateTime): string {
  const endText = start.hasSame(end, 'day')
    ? end.toLocaleString(DateTime.TIME_SIMPLE)
    : end.toLocaleString(DateTime.DATETIME_MED_WITH_WEEKDAY);
  const offsetNote =
    start.offset === end.offset ? '' : ` (UTC${start.toFormat('ZZ')} – UTC${end.toFormat('ZZ')})`;
  return `${start.toLocaleString(DateTime.TIME_SIMPLE)} – ${endText}${offsetNote}`;
}

/** Month/day over the weekday, weekend days in the calendar's weekend colours. */
export function SlotDayTile({ date, dense = false }: { date: DateTime; dense?: boolean }) {
  const weekdayColor =
    date.weekday === 7
      ? 'text-[var(--color-sunday)]'
      : date.weekday === 6
        ? 'text-[var(--color-saturday)]'
        : 'text-[var(--color-text-secondary)]';
  return (
    <span
      aria-hidden
      className={`flex shrink-0 flex-col items-center justify-center rounded-[var(--radius-sm)] bg-[var(--color-surface-inset)] leading-none tabular-nums ${dense ? 'h-10 w-11' : 'h-11 w-12'}`}
    >
      <span className="text-default font-semibold text-[var(--color-text-primary)]">
        {date.toFormat('M/d')}
      </span>
      <span className={`mt-1 text-micro font-medium ${weekdayColor}`}>{date.toFormat('ccc')}</span>
    </span>
  );
}

/**
 * A candidate as a date tile plus its time range, so a column of candidates
 * scans by day first. The full sentence stays available to assistive tech.
 */
export function SchedulePollSlot({
  startAt,
  endAt,
  timezone,
  dense = false,
}: {
  startAt: string;
  endAt: string;
  timezone: string;
  dense?: boolean;
}) {
  const locale = useUiStore((state) => state.locale);
  const start = DateTime.fromISO(startAt, { setZone: true }).setZone(timezone).setLocale(locale);
  const end = DateTime.fromISO(endAt, { setZone: true }).setZone(timezone).setLocale(locale);
  const otherYear = start.year !== DateTime.now().setZone(timezone).year;
  return (
    <span className="flex min-w-0 items-center gap-3">
      <SlotDayTile date={start} dense={dense} />
      <span className="min-w-0">
        <span className="sr-only">
          {formatSchedulePollSlotTime(startAt, endAt, timezone, locale)}
        </span>
        <span
          aria-hidden
          className="block truncate text-default font-medium text-[var(--color-text-primary)] tabular-nums"
        >
          {slotTimeRange(start, end)}
        </span>
        {otherYear && (
          <span aria-hidden className="block text-micro text-[var(--color-text-tertiary)]">
            {start.toFormat('yyyy')}
          </span>
        )}
      </span>
    </span>
  );
}

export function SchedulePollNotice({
  children,
  tone = 'info',
}: {
  children: ReactNode;
  tone?: 'info' | 'warning' | 'error';
}) {
  const toneClass =
    tone === 'warning' ? 'callout-warning' : tone === 'error' ? 'callout-danger' : '';
  return (
    <div
      role={tone === 'error' ? 'alert' : undefined}
      className={`callout ${toneClass} text-body text-[var(--color-text-primary)]`}
    >
      <svg
        aria-hidden
        width="16"
        height="16"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        className="callout-icon"
      >
        <circle cx="8" cy="8" r="6.25" />
        <path d={tone === 'info' ? 'M8 7.25v3.75M8 5h.01' : 'M8 4.75v3.75M8 11h.01'} />
      </svg>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/** Invitee picker: one checkable row per member, avatar beside the name. */
export function InviteeGrid({
  members,
  selectedIds,
  onToggle,
}: {
  members: PollMemberListItem[];
  selectedIds: string[];
  onToggle: (memberId: string, checked: boolean) => void;
}) {
  return (
    <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
      {members.map((member) => (
        <label
          key={member.id}
          className="choice-row flex min-h-12 cursor-pointer items-center gap-3 px-3 py-2"
        >
          <input
            type="checkbox"
            checked={selectedIds.includes(member.id)}
            onChange={(event) => onToggle(member.id, event.target.checked)}
            className="check-input"
          />
          <Avatar name={member.name} size="h-7 w-7" round className="text-footnote" />
          <span className="min-w-0 truncate text-body text-[var(--color-text-primary)]">
            {member.name}
          </span>
        </label>
      ))}
    </div>
  );
}
