import { createFileRoute } from '@tanstack/react-router';
import { DateTime } from 'luxon';
import { useEffect, useState } from 'react';
import { useT } from '@/i18n';
import { api } from '@/lib/api';
import { formatMonthYear, getWeekdayLabel } from '@/lib/date-utils';
import { useUiStore } from '@/stores/ui-store';

interface PublicCalendar {
  calendarId: string;
  name: string;
  color: string;
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

export const Route = createFileRoute('/embed/$token')({
  component: EmbeddedCalendarView,
});

/**
 * Chrome-less read-only month view designed to be embedded via <iframe>.
 * No app shell, no join action — purely public, view-only calendar data.
 */
function EmbeddedCalendarView() {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const { token } = Route.useParams();
  const [calendar, setCalendar] = useState<PublicCalendar | null>(null);
  const [events, setEvents] = useState<PublicEvent[]>([]);
  const [error, setError] = useState(false);
  const [currentMonth, setCurrentMonth] = useState(DateTime.now().startOf('month'));

  useEffect(() => {
    api
      .get<PublicCalendar>(`/share/${token}`, true)
      .then(setCalendar)
      .catch(() => setError(true));
  }, [token]);

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
      <div className="flex min-h-screen items-center justify-center bg-[var(--color-surface)] px-6 text-center text-body text-[var(--color-text-secondary)]">
        {t('share.linkExpired')}
      </div>
    );
  }

  if (!calendar) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--color-surface)] text-default text-[var(--color-text-secondary)]">
        {t('common.loading')}
      </div>
    );
  }

  const monthEnd = currentMonth.endOf('month');
  const gridStart = currentMonth.startOf('week');
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
    <div className="flex min-h-screen flex-col bg-[var(--color-surface)]">
      {/* Compact header */}
      <header
        className="flex items-center justify-between px-4 py-2.5 text-white"
        style={{ backgroundColor: calendar.color }}
      >
        <h1 className="truncate text-callout font-bold">{calendar.name}</h1>
        <span className="shrink-0 rounded-full bg-white/20 px-2 py-0.5 text-micro font-medium">
          {t('share.readOnly')}
        </span>
      </header>

      {/* Month navigation */}
      <div className="flex items-center justify-between border-b border-[var(--color-border)] px-4 py-2">
        <button
          type="button"
          aria-label="prev"
          onClick={() => setCurrentMonth(currentMonth.minus({ months: 1 }))}
          className="flex h-7 w-7 items-center justify-center rounded-full text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-secondary)]"
        >
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
          >
            <polyline points="15 18 9 12 15 6" />
          </svg>
        </button>
        <span className="text-footnote font-bold tabular-nums text-[var(--color-text-primary)]">
          {formatMonthYear(currentMonth, locale)}
        </span>
        <button
          type="button"
          aria-label="next"
          onClick={() => setCurrentMonth(currentMonth.plus({ months: 1 }))}
          className="flex h-7 w-7 items-center justify-center rounded-full text-[var(--color-text-secondary)] hover:bg-[var(--color-surface-secondary)]"
        >
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
          >
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </button>
      </div>

      {/* Grid */}
      <div className="flex-1">
        <div className="grid grid-cols-7 border-b border-[var(--color-border)]">
          {Array.from({ length: 7 }, (_, i) => {
            const label = getWeekdayLabel(i, locale);
            return (
              <div
                key={label}
                className="py-1.5 text-center text-micro font-medium"
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
                  className="min-h-[58px] p-1"
                  style={{
                    opacity: isCurrentMonth ? 1 : 0.35,
                    borderRight:
                      '1px solid color-mix(in srgb, var(--color-border) 30%, transparent)',
                  }}
                >
                  <div className="mb-0.5 flex items-center justify-center">
                    <span
                      className="flex h-5 w-5 items-center justify-center rounded-full text-micro tabular-nums"
                      style={{
                        backgroundColor: isToday ? calendar.color : 'transparent',
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
                      className="mb-0.5 truncate rounded px-1 py-0.5 text-micro leading-tight text-white"
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
    </div>
  );
}
