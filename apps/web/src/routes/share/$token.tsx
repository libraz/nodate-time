import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { DateTime } from 'luxon';
import { useCallback, useEffect, useState } from 'react';
import { useT } from '@/i18n';
import { ApiError, api, hasToken } from '@/lib/api';
import { formatMonthYear, getWeekdayLabel } from '@/lib/date-utils';
import { useUiStore } from '@/stores/ui-store';

interface PublicCalendar {
  calendarId: string;
  name: string;
  color: string;
  joinable: boolean;
}

interface PublicEvent {
  id: string;
  title: string;
  allDay: boolean;
  startAt: string;
  endAt: string;
  color: string;
  location?: string;
}

export const Route = createFileRoute('/share/$token')({
  component: SharedCalendarView,
});

function SharedCalendarView() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const { token } = Route.useParams();
  const navigate = useNavigate();
  const [calendar, setCalendar] = useState<PublicCalendar | null>(null);
  const [events, setEvents] = useState<PublicEvent[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [currentMonth, setCurrentMonth] = useState(DateTime.now().startOf('month'));
  const [joining, setJoining] = useState(false);
  const [joinError, setJoinError] = useState<string | null>(null);

  const handleJoin = useCallback(async () => {
    if (!hasToken()) {
      navigate({ to: '/login', search: { redirect: `/share/${token}` } });
      return;
    }
    setJoining(true);
    setJoinError(null);
    try {
      await api.post<{ calendarId: string; role: string }>(`/invites/${token}/accept`);
      navigate({ to: '/' });
    } catch (e) {
      if (e instanceof ApiError) {
        if (e.status === 409) {
          setJoinError(t('share.alreadyMember'));
        } else if (e.status === 404 || e.status === 410) {
          setJoinError(t('share.inviteInvalid'));
        } else {
          setJoinError(e.detail || t('share.joinFailed'));
        }
      } else {
        setJoinError(t('share.joinFailed'));
      }
    } finally {
      setJoining(false);
    }
  }, [token, navigate, t]);

  useEffect(() => {
    api
      .get<PublicCalendar>(`/share/${token}`, true)
      .then(setCalendar)
      .catch(() => setError(t('share.calendarNotFound')));
  }, [token, t]);

  useEffect(() => {
    if (!calendar) return;
    const start = currentMonth.toISODate();
    const end = currentMonth.endOf('month').toISODate();
    api
      .get<PublicEvent[]>(`/share/${token}/events?start=${start}&end=${end}`, true)
      .then(setEvents)
      .catch(() => setEvents([]));
  }, [token, calendar, currentMonth]);

  if (error) {
    return (
      <div className="app-bg flex min-h-screen items-center justify-center">
        <div className="glass-surface auth-card rounded-2xl px-8 py-12 text-center">
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
            {t('share.invalidLink')}
          </p>
          <p className="mt-2 text-body text-[var(--color-text-secondary)]">
            {t('share.linkExpired')}
          </p>
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

  // Build calendar grid days
  const monthStart = currentMonth;
  const monthEnd = currentMonth.endOf('month');
  const gridStart = monthStart.startOf('week');
  const gridEnd = monthEnd.endOf('week');

  const days: DateTime[] = [];
  let d = gridStart;
  while (d <= gridEnd) {
    days.push(d);
    d = d.plus({ days: 1 });
  }

  const weeks: DateTime[][] = [];
  for (let i = 0; i < days.length; i += 7) {
    weeks.push(days.slice(i, i + 7));
  }

  const getEventsForDay = (day: DateTime) => {
    const dayStart = day.startOf('day');
    const dayEnd = day.endOf('day');
    return events.filter((e) => {
      const eStart = DateTime.fromISO(e.startAt);
      const eEnd = DateTime.fromISO(e.endAt);
      return eStart < dayEnd && eEnd > dayStart;
    });
  };

  const today = DateTime.now();

  return (
    <div className="app-bg flex min-h-screen flex-col">
      <div className="auth-card mx-auto flex w-full max-w-[680px] flex-1 flex-col overflow-hidden sm:my-6 sm:flex-initial sm:rounded-3xl sm:shadow-xl sm:ring-1 sm:ring-[var(--color-border)]">
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
          <span className="rounded-full bg-white/20 px-3 py-1 text-caption font-medium">
            {t('share.readOnly')}
          </span>
        </header>

        {/* Month navigation */}
        <div className="flex items-center justify-between bg-[var(--color-surface)] px-4 py-3 shadow-sm sm:px-6">
          <button
            type="button"
            onClick={() => setCurrentMonth(currentMonth.minus({ months: 1 }))}
            className="flex h-8 w-8 items-center justify-center rounded-full text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-secondary)]"
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
            onClick={() => setCurrentMonth(currentMonth.plus({ months: 1 }))}
            className="flex h-8 w-8 items-center justify-center rounded-full text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-secondary)]"
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
                borderBottom: '1px solid color-mix(in srgb, var(--color-border) 50%, transparent)',
              }}
            >
              {week.map((day) => {
                const isCurrentMonth = day.month === currentMonth.month;
                const isToday = day.hasSame(today, 'day');
                const dayEvents = getEventsForDay(day);

                return (
                  <div
                    key={day.toISO()}
                    className="min-h-[70px] p-1 last:border-r-0 sm:min-h-[90px] sm:p-2"
                    style={{
                      opacity: isCurrentMonth ? 1 : 0.35,
                      borderRight:
                        '1px solid color-mix(in srgb, var(--color-border) 30%, transparent)',
                    }}
                  >
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
                        {evt.title}
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
              className="btn-primary h-11 w-full rounded-xl text-callout font-bold"
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
