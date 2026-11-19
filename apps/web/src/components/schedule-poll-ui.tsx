import { Link } from '@tanstack/react-router';
import { DateTime } from 'luxon';
import type { ReactNode } from 'react';
import { BrandRow } from '@/components/brand-mark';
import { useT } from '@/i18n';
import type { PollAvailability, SchedulePoll } from '@/lib/schedule-polls';
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
      <div className="mx-auto flex min-h-screen max-w-[1120px] flex-col px-4 py-5 sm:px-6 sm:py-8">
        <header className="mb-6 flex items-start justify-between gap-4 border-b border-[var(--color-border)] pb-5">
          <div className="min-w-0">
            <div className="mb-4">
              <BrandRow />
            </div>
            {eyebrow && (
              <p className="mb-1 text-footnote font-semibold uppercase tracking-wide text-[var(--color-text-secondary)]">
                {eyebrow}
              </p>
            )}
            <h1 className="text-display font-bold text-[var(--color-text-primary)] sm:text-hero">
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
        <main className="flex-1">{children}</main>
      </div>
    </div>
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
  return (
    <span className="inline-flex rounded-full bg-[var(--color-surface-inset)] px-2.5 py-1 text-footnote font-semibold text-[var(--color-text-secondary)]">
      {copy}
    </span>
  );
}

export function AvailabilityBadge({ availability }: { availability: PollAvailability }) {
  const t = useT();
  const classes =
    availability === 'open'
      ? 'border-[color-mix(in_srgb,var(--color-accent)_28%,var(--color-border))] bg-[var(--color-accent-bg)] text-[var(--color-accent)]'
      : availability === 'soft'
        ? 'border-[color-mix(in_srgb,var(--color-warning)_28%,var(--color-border))] bg-[var(--color-warning-bg)] text-[var(--color-warning)]'
        : 'border-[color-mix(in_srgb,var(--color-danger)_25%,var(--color-border))] bg-[var(--color-danger-bg)] text-[var(--color-danger)]';
  const label =
    availability === 'open'
      ? t('schedulePoll.availabilityOpen')
      : availability === 'soft'
        ? t('schedulePoll.availabilitySoft')
        : t('schedulePoll.availabilityHard');
  return (
    <span
      className={`inline-flex rounded-full border px-2 py-0.5 text-micro font-semibold ${classes}`}
      style={
        availability === 'soft'
          ? {
              borderColor:
                'color-mix(in srgb, var(--color-warning, #e5a00d) 28%, var(--color-border))',
              backgroundColor:
                'color-mix(in srgb, var(--color-warning, #e5a00d) 10%, var(--color-surface))',
              color: 'var(--color-warning, #e5a00d)',
            }
          : undefined
      }
    >
      {label}
    </span>
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

export function SchedulePollSlotTime({
  startAt,
  endAt,
  timezone,
}: {
  startAt: string;
  endAt: string;
  timezone: string;
}) {
  const locale = useUiStore((state) => state.locale);
  return (
    <span className="tabular-nums">
      {formatSchedulePollSlotTime(startAt, endAt, timezone, locale)}
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
  const styles = {
    info: 'border-[color-mix(in_srgb,var(--color-accent)_28%,var(--color-border))] bg-[var(--color-accent-bg)] text-[var(--color-text-primary)]',
    warning:
      'border-[var(--color-border)] bg-[var(--color-surface-inset)] text-[var(--color-text-primary)]',
    error:
      'border-[color-mix(in_srgb,var(--color-danger)_28%,var(--color-border))] bg-[var(--color-danger-bg)] text-[var(--color-text-primary)]',
  };
  const warningStyle =
    tone === 'warning'
      ? {
          borderColor: 'color-mix(in srgb, var(--color-warning, #e5a00d) 28%, var(--color-border))',
          backgroundColor:
            'color-mix(in srgb, var(--color-warning, #e5a00d) 10%, var(--color-surface))',
        }
      : undefined;
  return (
    <div className={`rounded-xl border px-4 py-3 text-body ${styles[tone]}`} style={warningStyle}>
      {children}
    </div>
  );
}
