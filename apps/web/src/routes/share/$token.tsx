import { createFileRoute, useNavigate } from '@tanstack/react-router';
import type { DateTime } from 'luxon';
import { useCallback, useEffect, useRef, useState } from 'react';
import { formatSheetDay, PublicDaySheet } from '@/components/public-day-sheet';
import { useT } from '@/i18n';
import { errorMessage, hasToken } from '@/lib/api';
import { client } from '@/lib/client';
import { formatMonthYear, getWeekdayLabel, nowInZone } from '@/lib/date-utils';
import {
  type PublicCalendar,
  publishesEvents,
  type ShareFailure,
  shareFailureOf,
} from '@/lib/public-calendar';
import { toast } from '@/lib/toast';
import { usePublicEvents, usePublicMonth, usePublicMonthGrid } from '@/lib/use-public-events';
import { useUiStore } from '@/stores/ui-store';

/** `join=1` marks a return from sign-in with the intent to accept the invite. */
export interface ShareSearch {
  join?: string | undefined;
}

export const Route = createFileRoute('/share/$token')({
  validateSearch: (search: Record<string, unknown>): ShareSearch => ({
    join: search.join === '1' || search.join === 1 ? '1' : undefined,
  }),
  component: SharedCalendarView,
});

/** Exported for testing: what a link publishes decides what this page draws. */
export function SharedCalendarView() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const timezone = useUiStore((s) => s.timezone);
  const { token } = Route.useParams();
  const { join } = Route.useSearch();
  const navigate = useNavigate();
  const [calendar, setCalendar] = useState<PublicCalendar | null>(null);
  // Whether the calendar could be read at all, kept apart from why. The words
  // shown come from the reason below; holding a sentence here as well would
  // put the translator in this callback, and `useT` hands back a new function
  // on every render -- enough to make the load depend on itself.
  const [failed, setFailed] = useState(false);
  const [failure, setFailure] = useState<ShareFailure | null>(null);
  const { month: currentMonth, shiftMonth } = usePublicMonth(timezone);
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);
  const [openDay, setOpenDay] = useState<DateTime | null>(null);
  const autoJoined = useRef(false);

  const handleJoin = useCallback(async () => {
    if (!hasToken()) {
      navigate({
        to: '/login',
        search: { mode: 'register', redirect: `/share/${token}?join=1` },
      });
      return;
    }
    setJoining(true);
    setJoinError(null);
    try {
      await client.POST('/invites/{token}/accept', { params: { path: { token } } });
      toast.success(t('invite.joined', { name: calendar?.name ?? '' }));
      navigate({ to: '/' });
    } catch (e) {
      // Every refusal the server can give here carries a code, and the code is
      // what has a sentence in the reader's language behind it: a link that has
      // run out, one that never existed, and a public link that publishes a
      // calendar without ever admitting anyone to it. Branching on the status
      // instead left that last one showing the server's English, and answered a
      // 409 the server does not send -- an existing member is readmitted
      // silently rather than refused.
      setJoinError(errorMessage(e, t('invite.joinFailed')));
    } finally {
      setJoining(false);
    }
  }, [token, navigate, t, calendar?.name]);

  // Depends on the token and nothing else. Every response is a fresh object,
  // so a load that re-created itself on each render asked again for what it
  // had just been given, and went on asking.
  const loadCalendar = useCallback(() => {
    client
      .GET('/share/{token}', { params: { path: { token } } })
      .then(({ data }) => {
        setCalendar(data ?? null);
        setFailed(false);
        setFailure(null);
      })
      .catch((e: unknown) => {
        setFailure(shareFailureOf(e));
        setFailed(true);
      });
  }, [token]);

  useEffect(() => {
    loadCalendar();
  }, [loadCalendar]);

  // Coming back from sign-in with join=1 accepts once, without another tap.
  useEffect(() => {
    if (join !== '1' || !calendar?.joinable || autoJoined.current || !hasToken()) return;
    autoJoined.current = true;
    void handleJoin();
  }, [join, calendar, handleJoin]);

  const {
    events,
    hasMore,
    failure: eventsFailure,
    retry: retryEvents,
  } = usePublicEvents(token, !!calendar && publishesEvents(calendar), currentMonth, timezone);

  const { weeks, eventsOn } = usePublicMonthGrid(events, currentMonth, timezone);

  // A month whose events did not arrive is not an empty month: it is reported
  // the same way a calendar that did not arrive is.
  const shownFailure = failed ? failure : eventsFailure;
  if (shownFailure) {
    return (
      <div className="app-bg flex min-h-screen items-center justify-center">
        <div className="glass-surface auth-card rounded-[var(--radius-xl)] px-8 py-12 text-center">
          <svg
            className="mx-auto mb-4 h-12 w-12 text-[var(--color-text-tertiary)]"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="15" y1="9" x2="9" y2="15" />
            <line x1="9" y1="9" x2="15" y2="15" />
          </svg>
          <p className="text-subhead font-medium text-[var(--color-text-primary)]">
            {shownFailure === 'busy' ? t('share.temporarilyUnavailable') : t('share.invalidLink')}
          </p>
          <p className="mt-2 text-body text-[var(--color-text-secondary)]">
            {shownFailure === 'busy' ? t('share.tryAgainShortly') : t('share.linkExpired')}
          </p>
          {shownFailure === 'busy' && (
            <button
              type="button"
              onClick={() => {
                if (!failed) {
                  retryEvents();
                  return;
                }
                setFailed(false);
                setFailure(null);
                loadCalendar();
              }}
              className="btn-secondary mt-4 h-10 px-5 text-default"
            >
              {t('share.retry')}
            </button>
          )}
        </div>
      </div>
    );
  }

  if (!calendar) {
    return (
      <div className="app-bg flex min-h-screen items-center justify-center">
        <div className="text-default text-[var(--color-text-secondary)]">{t('common.loading')}</div>
      </div>
    );
  }

  const showsGrid = publishesEvents(calendar);
  const today = nowInZone(timezone);

  return (
    <div className="app-bg flex min-h-screen flex-col">
      <div className="auth-card mx-auto flex w-full max-w-[680px] flex-1 flex-col overflow-hidden sm:my-6 sm:flex-initial sm:rounded-[var(--radius-xl)] sm:shadow-xl sm:ring-1 sm:ring-[var(--color-border)]">
        {/* Header */}
        <header
          className="flex items-center justify-between px-4 py-3 text-white shadow-sm sm:px-6"
          style={{ backgroundColor: calendar.color }}
        >
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <rect x="3" y="4" width="18" height="18" rx="2" />
              <line x1="16" y1="2" x2="16" y2="6" />
              <line x1="8" y1="2" x2="8" y2="6" />
              <line x1="3" y1="10" x2="21" y2="10" />
            </svg>
            <h1 className="text-subhead font-bold sm:text-title">{calendar.name}</h1>
          </div>
          {showsGrid && (
            <span className="rounded-full bg-white/20 px-3 py-1 text-caption font-medium">
              {t('share.readOnly')}
            </span>
          )}
        </header>

        {showsGrid && (
          <>
            {/* Month navigation */}
            <div className="flex items-center justify-between bg-[var(--color-surface)] px-4 py-3 shadow-sm sm:px-6">
              <button
                type="button"
                onClick={() => shiftMonth(-1)}
                aria-label={t('calendar.prevMonth')}
                className="icon-btn h-8 w-8 rounded-full"
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.5"
                >
                  <polyline points="15 18 9 12 15 6" />
                </svg>
              </button>
              <span className="text-callout font-bold tabular-nums text-[var(--color-text-primary)]">
                {formatMonthYear(currentMonth, locale)}
              </span>
              <button
                type="button"
                onClick={() => shiftMonth(1)}
                aria-label={t('calendar.nextMonth')}
                className="icon-btn h-8 w-8 rounded-full"
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.5"
                >
                  <polyline points="9 18 15 12 9 6" />
                </svg>
              </button>
            </div>

            {hasMore && (
              <output className="block bg-[var(--color-surface-inset)] px-4 py-2 text-footnote text-[var(--color-text-secondary)] sm:px-6">
                {t('calendar.eventsTruncated')}
              </output>
            )}

            {/* Calendar grid */}
            <div className="flex-1 bg-[var(--color-surface)]">
              {/* Day of week header */}
              <div className="grid grid-cols-7 border-b border-[var(--color-border)]">
                {Array.from({ length: 7 }, (_, i) => {
                  const label = getWeekdayLabel(i, locale);
                  return (
                    <div
                      key={label}
                      className="py-2 text-center text-footnote font-medium"
                      style={{
                        color:
                          i === 0
                            ? 'var(--color-sunday)'
                            : i === 6
                              ? 'var(--color-saturday)'
                              : 'var(--color-text-secondary)',
                      }}
                    >
                      {label}
                    </div>
                  );
                })}
              </div>

              {/* Weeks */}
              {weeks.map((week) => (
                <div
                  key={week[0]?.toISO()}
                  className="grid grid-cols-7"
                  style={{
                    borderBottom:
                      '1px solid color-mix(in srgb, var(--color-border) 50%, transparent)',
                  }}
                >
                  {week.map((day) => {
                    const isCurrentMonth = day.month === currentMonth.month;
                    const isToday = day.hasSame(today, 'day');
                    const dayEvents = eventsOn(day);

                    return (
                      <div
                        key={day.toISO()}
                        className="relative min-h-[70px] p-1 last:border-r-0 sm:min-h-[90px] sm:p-2"
                        style={{
                          opacity: isCurrentMonth ? 1 : 0.35,
                          borderRight:
                            '1px solid color-mix(in srgb, var(--color-border) 30%, transparent)',
                        }}
                      >
                        <button
                          type="button"
                          aria-label={formatSheetDay(day, locale)}
                          onClick={() => setOpenDay(day)}
                          className="absolute inset-0"
                        />
                        <div className="mb-1 flex items-center justify-center">
                          <span
                            className="flex h-6 w-6 items-center justify-center rounded-full text-footnote tabular-nums sm:text-body"
                            style={{
                              backgroundColor: isToday ? calendar.color : 'transparent',
                              boxShadow: isToday
                                ? `0 2px 8px color-mix(in srgb, ${calendar.color} 45%, transparent)`
                                : undefined,
                              color: isToday
                                ? '#fff'
                                : day.weekday === 7
                                  ? 'var(--color-sunday)'
                                  : day.weekday === 6
                                    ? 'var(--color-saturday)'
                                    : 'var(--color-text-primary)',
                              fontWeight: isToday ? 700 : 400,
                            }}
                          >
                            {day.day}
                          </span>
                        </div>
                        {dayEvents.slice(0, 3).map((evt) => (
                          <div
                            key={evt.id}
                            className="mb-0.5 truncate rounded px-1 py-0.5 text-micro leading-tight text-white sm:text-caption"
                            style={{ backgroundColor: evt.color || calendar.color }}
                          >
                            {evt.detailsHidden ? t('share.busy') : evt.title}
                          </div>
                        ))}
                        {dayEvents.length > 3 && (
                          <div className="text-center text-micro tabular-nums text-[var(--color-text-secondary)]">
                            +{dayEvents.length - 3}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              ))}
            </div>

            {openDay && (
              <PublicDaySheet
                day={openDay}
                events={eventsOn(openDay)}
                timezone={timezone}
                locale={locale}
                fallbackColor={calendar.color}
                onClose={() => setOpenDay(null)}
              />
            )}
          </>
        )}

        {/* A used-up join link still shows the calendar it named, so say why
            the button is gone rather than leaving the visitor to guess. */}
        {calendar.spent && (
          <div className="bg-[var(--color-surface)] px-4 py-5 shadow-inner sm:px-6">
            <p className="text-center text-body text-[var(--color-text-secondary)]">
              {t('share.linkUsedUp')}
            </p>
          </div>
        )}

        {/* Join section — hidden for public, read-only links that cannot be joined */}
        {calendar.joinable && (
          <div className="bg-[var(--color-surface)] px-4 py-5 shadow-inner sm:px-6">
            {joinError && (
              <div className="mb-3 rounded-xl bg-[var(--color-danger-bg)] px-4 py-3 text-center text-body text-[var(--color-danger)]">
                {joinError}
              </div>
            )}
            <button
              type="button"
              onClick={handleJoin}
              disabled={joining}
              className="btn-primary h-11 w-full text-callout font-bold"
            >
              {joining ? t('share.joining') : t('share.joinCalendar')}
            </button>
          </div>
        )}

        {/* Footer */}
        <footer className="bg-[var(--color-surface)] py-4 text-center text-footnote text-[var(--color-text-tertiary)]">
          {t('share.poweredBy')}
        </footer>
      </div>
    </div>
  );
}
