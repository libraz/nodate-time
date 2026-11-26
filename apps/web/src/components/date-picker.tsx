import { DateTime } from 'luxon';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  Chevron,
  handlePopupKeys,
  PickerSurface,
  useOutsideClose,
  useSheetPicker,
} from '@/components/picker-surface';
import { useT } from '@/i18n';
import {
  columnOf,
  formatMonthYear,
  formatYear,
  getWeekdayLabel,
  isToday,
  weekdayOrder,
} from '@/lib/date-utils';
import { getHoliday } from '@/lib/holidays';
import { useHolidayLoader } from '@/lib/use-holidays';
import { useUiStore } from '@/stores/ui-store';

/** Six weeks of seven days: every month fits, so the panel never changes height. */
const PICKER_GRID_CELLS = 42;

export interface DateRangeLink {
  role: 'start' | 'end';
  other: DateTime;
}

interface DatePickerDropdownProps {
  value: DateTime;
  onChange: (date: DateTime) => void;
  onClose: () => void;
  minDate?: DateTime;
  range?: DateRangeLink;
  /** Heads the bottom sheet on a phone, where the field itself is covered. */
  title?: string;
  style: React.CSSProperties;
  floatingRef: React.RefObject<HTMLDivElement | null>;
  /** The field that opened the picker; takes focus back when the picker closes. */
  returnFocusRef: React.RefObject<HTMLElement | null>;
}

export function DatePickerDropdown({
  value,
  onChange,
  onClose,
  minDate,
  range,
  title,
  style,
  floatingRef,
  returnFocusRef,
}: DatePickerDropdownProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const holidaysCountry = useUiStore((s) => s.holidaysCountry);
  const weekStart = useUiStore((s) => s.weekStart);
  const sheet = useSheetPicker();
  const [viewMonth, setViewMonth] = useState(value.startOf('month'));
  const [mode, setMode] = useState<'days' | 'months'>('days');
  // Which way the last month turn went, so the new grid slides in from that side.
  const [turn, setTurn] = useState(0);
  // The day the arrow keys are on; only that day is a Tab stop.
  const [focusDay, setFocusDay] = useState(value);
  const [hoverDay, setHoverDay] = useState<DateTime | null>(null);
  const gridRef = useRef<HTMLDivElement>(null);

  useOutsideClose([floatingRef], onClose, true);
  useHolidayLoader(holidaysCountry, [viewMonth.year]);

  const closeToField = () => {
    onClose();
    returnFocusRef.current?.focus();
  };

  // Opening, and every arrow move, puts focus on the day it names.
  useEffect(() => {
    gridRef.current
      ?.querySelector<HTMLElement>(`[data-day="${focusDay.toISODate()}"]`)
      ?.focus({ preventScroll: true });
  }, [focusDay]);

  const minDay = minDate?.startOf('day');
  const minMonth = minDate?.setZone(value.zone).startOf('month');
  const todayDate = DateTime.now().setZone(value.zone).startOf('day');
  const isBeforeMin = (day: DateTime) => (minDay ? day < minDay : false);

  const showMonth = (month: DateTime) => {
    setTurn(month > viewMonth ? 1 : month < viewMonth ? -1 : 0);
    setViewMonth(month.startOf('month'));
  };

  const moveFocus = (e: React.KeyboardEvent, next: DateTime) => {
    e.preventDefault();
    if (isBeforeMin(next)) return;
    setFocusDay(next);
    if (!next.hasSame(viewMonth, 'month')) showMonth(next);
  };

  const handleGridKey = (e: React.KeyboardEvent) => {
    const next = (() => {
      switch (e.key) {
        case 'ArrowLeft':
          return focusDay.minus({ days: 1 });
        case 'ArrowRight':
          return focusDay.plus({ days: 1 });
        case 'ArrowUp':
          return focusDay.minus({ days: 7 });
        case 'ArrowDown':
          return focusDay.plus({ days: 7 });
        case 'PageUp':
          return focusDay.minus({ months: 1 });
        case 'PageDown':
          return focusDay.plus({ months: 1 });
        default:
          return null;
      }
    })();
    if (next) moveFocus(e, next);
  };

  const weekdays = weekdayOrder(weekStart).map((dow) => {
    const label = getWeekdayLabel(dow, locale);
    return { dow, label: locale === 'ja' ? label : label.slice(0, 2) };
  });

  const { emptySlots, calendarDays, trailingSlots } = useMemo(() => {
    const first = viewMonth.startOf('month');
    const startOffset = columnOf(first, weekStart);
    const daysInMonth = first.daysInMonth ?? 30;
    const days: DateTime[] = [];
    for (let d = 1; d <= daysInMonth; d++) days.push(first.set({ day: d }));
    // Always six rows, so the panel keeps its height from month to month.
    return {
      emptySlots: startOffset,
      calendarDays: days,
      trailingSlots: PICKER_GRID_CELLS - startOffset - daysInMonth,
    };
  }, [viewMonth, weekStart]);

  const handleSelect = (day: DateTime) => {
    if (isBeforeMin(day)) return;
    onChange(day);
    closeToField();
  };

  const stepView = (step: 1 | -1) =>
    mode === 'days'
      ? showMonth(viewMonth.plus({ months: step }))
      : setViewMonth((m) => m.plus({ years: step }));
  const atFirstPage = minMonth
    ? mode === 'days'
      ? viewMonth <= minMonth
      : viewMonth.year <= minMonth.year
    : false;

  // The span the field would produce: its own end follows the pointer.
  const span = (() => {
    if (!range) return null;
    const own = (hoverDay ?? value).startOf('day');
    const other = range.other.startOf('day');
    const [start, end] = range.role === 'start' ? [own, other] : [other, own];
    return start <= end ? { start, end } : null;
  })();

  const shortcuts = [
    { label: t('calendar.today'), day: todayDate },
    { label: t('picker.tomorrow'), day: todayDate.plus({ days: 1 }) },
    { label: t('picker.inAWeek'), day: todayDate.plus({ weeks: 1 }) },
  ];

  const cellHeight = sheet ? 'h-11' : 'h-10';
  const dayButton = sheet ? 'h-11 w-11 text-callout' : 'h-9 w-9 text-body';
  const tabDay = focusDay.hasSame(viewMonth, 'month') ? focusDay : viewMonth.startOf('month');

  return (
    <PickerSurface
      sheet={sheet}
      title={title}
      floatingRef={floatingRef}
      style={style}
      popoverClassName="w-[296px] p-3"
      onKeyDown={(e) => handlePopupKeys(e, closeToField)}
    >
      <div className="mb-1 flex items-center justify-between">
        <button
          type="button"
          onClick={() => setMode((m) => (m === 'days' ? 'months' : 'days'))}
          aria-expanded={mode === 'months'}
          className="-ml-1 inline-flex h-8 items-center gap-1 rounded-[var(--radius-control)] px-2 text-default font-semibold tabular-nums text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-hover)]"
        >
          {mode === 'days' ? formatMonthYear(viewMonth, locale) : formatYear(viewMonth, locale)}
          <span
            className={`text-[var(--color-text-tertiary)] transition-transform ${mode === 'months' ? 'rotate-180' : ''}`}
          >
            <Chevron dir="down" />
          </span>
        </button>
        <div className="flex items-center">
          <button
            type="button"
            onClick={() => stepView(-1)}
            disabled={atFirstPage}
            className="icon-btn h-8 w-8 text-[var(--color-text-primary)] disabled:opacity-30"
            aria-label={mode === 'days' ? t('calendar.prevMonth') : t('picker.prevYear')}
          >
            <Chevron dir="left" />
          </button>
          <button
            type="button"
            onClick={() => stepView(1)}
            className="icon-btn h-8 w-8 text-[var(--color-text-primary)]"
            aria-label={mode === 'days' ? t('calendar.nextMonth') : t('picker.nextYear')}
          >
            <Chevron dir="right" />
          </button>
        </div>
      </div>

      {mode === 'months' ? (
        <div
          className={`grid grid-cols-3 grid-rows-4 gap-1.5 ${sheet ? 'h-[292px]' : 'h-[268px]'}`}
        >
          {Array.from({ length: 12 }, (_, i) => {
            const month = viewMonth.set({ month: i + 1 }).startOf('month');
            const disabled = minMonth ? month < minMonth : false;
            const selected = month.hasSame(value, 'month');
            const current = month.hasSame(todayDate, 'month');
            return (
              <button
                key={month.month}
                type="button"
                disabled={disabled}
                aria-pressed={selected}
                onClick={() => {
                  showMonth(month);
                  setMode('days');
                }}
                className={`rounded-[var(--radius-control)] text-body font-medium transition-colors disabled:opacity-30 ${
                  selected
                    ? 'bg-[var(--color-accent)] text-[var(--color-text-on-accent)]'
                    : current
                      ? 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)] enabled:hover:bg-[var(--color-accent-subtle)]'
                      : 'text-[var(--color-text-primary)] enabled:hover:bg-[var(--color-hover)]'
                }`}
              >
                {locale === 'en' ? month.setLocale('en').toFormat('MMM') : `${month.month}月`}
              </button>
            );
          })}
        </div>
      ) : (
        <>
          <div className="grid grid-cols-7">
            {weekdays.map(({ dow, label }) => (
              <div
                key={dow}
                className="py-1 text-center text-caption font-medium"
                style={{
                  color:
                    dow === 0
                      ? 'var(--color-sunday)'
                      : dow === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-tertiary)',
                }}
              >
                {label}
              </div>
            ))}
          </div>
          {/* biome-ignore lint/a11y/noStaticElementInteractions: the grid only receives key events bubbling from its buttons */}
          <div
            key={viewMonth.toISODate()}
            ref={gridRef}
            onKeyDown={handleGridKey}
            onMouseLeave={() => setHoverDay(null)}
            data-dir={turn}
            className="picker-turn grid grid-cols-7"
          >
            {Array.from({ length: emptySlots }, (_, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholder cells have no identity
              <div key={`lead-${i}`} className={cellHeight} />
            ))}
            {calendarDays.map((day) => {
              const iso = day.toISODate() ?? '';
              const isSelected = day.hasSame(value, 'day');
              // The grid is built from `value`, so "today" has to be read in the
              // same zone that value carries rather than the browser's.
              const today = isToday(day, value.zoneName ?? undefined);
              const isDisabled = isBeforeMin(day);
              const holiday = getHoliday(holidaysCountry, iso);
              const dayOfWeek = day.weekday % 7;
              const inSpan = span && day >= span.start && day <= span.end;
              const spanStart = inSpan && day.hasSame(span.start, 'day');
              const spanEnd = inSpan && day.hasSame(span.end, 'day');
              const isOtherEnd = range ? day.hasSame(range.other, 'day') : false;
              const column = columnOf(day, weekStart);
              const openLeft = !spanStart && (column === 0 || day.day === 1);
              const openRight = !spanEnd && (column === 6 || day.day === calendarDays.length);
              return (
                <div
                  key={iso}
                  className={`relative flex items-center justify-center ${cellHeight}`}
                >
                  {inSpan && !(spanStart && spanEnd) && (
                    <span
                      aria-hidden
                      className="absolute inset-y-1 bg-[var(--color-accent-bg)]"
                      style={{
                        left: spanStart ? '50%' : 0,
                        right: spanEnd ? '50%' : 0,
                        // The band breaks at a row or month edge; round it there.
                        borderTopLeftRadius: openLeft ? 999 : 0,
                        borderBottomLeftRadius: openLeft ? 999 : 0,
                        borderTopRightRadius: openRight ? 999 : 0,
                        borderBottomRightRadius: openRight ? 999 : 0,
                      }}
                    />
                  )}
                  <button
                    type="button"
                    onClick={() => handleSelect(day)}
                    onMouseEnter={() => !isDisabled && setHoverDay(day)}
                    disabled={isDisabled}
                    data-day={iso}
                    tabIndex={day.hasSame(tabDay, 'day') ? 0 : -1}
                    aria-label={
                      holiday
                        ? `${day.toFormat('yyyy-MM-dd')} ${holiday.name}`
                        : day.toFormat('yyyy-MM-dd')
                    }
                    aria-pressed={isSelected}
                    aria-current={today ? 'date' : undefined}
                    title={holiday?.name}
                    className={`relative flex items-center justify-center rounded-full tabular-nums transition-colors disabled:cursor-default ${dayButton} ${
                      isSelected
                        ? 'bg-[var(--color-accent)] shadow-[0_2px_8px_-2px_color-mix(in_srgb,var(--color-accent)_60%,transparent)]'
                        : isOtherEnd
                          ? 'ring-[1.5px] ring-[var(--color-accent)] ring-inset enabled:hover:bg-[var(--color-hover)]'
                          : 'enabled:hover:bg-[var(--color-hover)]'
                    }`}
                    style={{
                      color: isSelected
                        ? 'var(--color-text-on-accent)'
                        : dayOfWeek === 0 || holiday
                          ? 'var(--color-sunday)'
                          : dayOfWeek === 6
                            ? 'var(--color-saturday)'
                            : 'var(--color-text-primary)',
                      fontWeight: isSelected || today ? 600 : 400,
                      opacity: isDisabled ? 0.32 : 1,
                    }}
                  >
                    {day.day}
                    {today && (
                      <span
                        aria-hidden
                        className="absolute bottom-1 left-1/2 h-1 w-1 -translate-x-1/2 rounded-full"
                        style={{
                          backgroundColor: isSelected
                            ? 'var(--color-text-on-accent)'
                            : 'var(--color-accent)',
                        }}
                      />
                    )}
                  </button>
                </div>
              );
            })}
            {Array.from({ length: trailingSlots }, (_, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholder cells have no identity
              <div key={`pad-${i}`} className={cellHeight} />
            ))}
          </div>
        </>
      )}

      <div className="mt-2 flex items-center gap-1.5 border-t border-[var(--color-separator)] pt-2.5">
        {shortcuts.map(({ label, day }) => (
          <button
            key={label}
            type="button"
            onClick={() => handleSelect(day)}
            disabled={isBeforeMin(day)}
            className={`rounded-full bg-[var(--color-surface-inset)] px-3 text-footnote font-medium text-[var(--color-text-secondary)] transition-colors enabled:hover:bg-[var(--color-accent-bg)] enabled:hover:text-[var(--color-accent-ink)] disabled:opacity-40 ${sheet ? 'h-9' : 'h-7'}`}
          >
            {label}
          </button>
        ))}
      </div>
    </PickerSurface>
  );
}
