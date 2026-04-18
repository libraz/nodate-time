import { useUiStore } from '@/stores/ui-store';
import { DateTime } from 'luxon';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

/* ============================================
   useFloating — positions a floating element
   relative to an anchor, rendered via portal
   so it escapes overflow:hidden containers.
   ============================================ */

function useFloating(open: boolean) {
  const anchorRef = useRef<HTMLButtonElement>(null);
  const floatingRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ top: 0, left: 0, direction: 'below' as 'below' | 'above' });

  useLayoutEffect(() => {
    if (!open || !anchorRef.current) return;
    const rect = anchorRef.current.getBoundingClientRect();
    const spaceBelow = window.innerHeight - rect.bottom;
    const floatingHeight = floatingRef.current?.offsetHeight ?? 300;
    const goAbove = spaceBelow < floatingHeight + 8 && rect.top > spaceBelow;

    setPos({
      top: goAbove ? rect.top : rect.bottom + 4,
      left: rect.left,
      direction: goAbove ? 'above' : 'below',
    });
  }, [open]);

  return { anchorRef, floatingRef, pos };
}

/* ============================================
   DatePicker — inline calendar for date selection
   ============================================ */

const WEEKDAYS_JA = ['日', '月', '火', '水', '木', '金', '土'];
const WEEKDAYS_EN = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

interface DatePickerDropdownProps {
  value: DateTime;
  onChange: (date: DateTime) => void;
  onClose: () => void;
  minDate?: DateTime;
  style: React.CSSProperties;
  floatingRef: React.RefObject<HTMLDivElement | null>;
}

function DatePickerDropdown({
  value,
  onChange,
  onClose,
  minDate,
  style,
  floatingRef,
}: DatePickerDropdownProps) {
  const locale = useUiStore((s) => s.locale);
  const [viewMonth, setViewMonth] = useState(value.startOf('month'));

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (floatingRef.current && !floatingRef.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose, floatingRef]);

  const weekdays = locale === 'ja' ? WEEKDAYS_JA : WEEKDAYS_EN;

  const { emptySlots, calendarDays } = useMemo(() => {
    const first = viewMonth.startOf('month');
    const startOffset = first.weekday % 7;
    const daysInMonth = first.daysInMonth ?? 30;
    const days: DateTime[] = [];
    for (let d = 1; d <= daysInMonth; d++) days.push(first.set({ day: d }));
    return { emptySlots: startOffset, calendarDays: days };
  }, [viewMonth]);

  const monthLabel =
    locale === 'ja' ? `${viewMonth.year}年${viewMonth.month}月` : viewMonth.toFormat('MMMM yyyy');

  const handleSelect = (day: DateTime) => {
    if (minDate && day < minDate.startOf('day')) return;
    onChange(day);
    onClose();
  };

  return createPortal(
    <div
      ref={floatingRef}
      className="fixed z-[9999] w-[280px] rounded-2xl bg-[var(--color-surface-elevated)] p-3 ring-1 ring-[var(--color-border)]"
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {/* Month navigation */}
      <div className="mb-2 flex items-center justify-between">
        <button
          type="button"
          onClick={() => setViewMonth((m) => m.minus({ months: 1 }))}
          className="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-[var(--color-hover)]"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-primary)"
            strokeWidth="2"
          >
            <path d="M15 18l-6-6 6-6" />
          </svg>
        </button>
        <span className="text-[14px] font-semibold text-[var(--color-text-primary)]">
          {monthLabel}
        </span>
        <button
          type="button"
          onClick={() => setViewMonth((m) => m.plus({ months: 1 }))}
          className="flex h-8 w-8 items-center justify-center rounded-lg hover:bg-[var(--color-hover)]"
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--color-text-primary)"
            strokeWidth="2"
          >
            <path d="M9 18l6-6-6-6" />
          </svg>
        </button>
      </div>

      {/* Weekday header */}
      <div className="mb-1 grid grid-cols-7 gap-0">
        {weekdays.map((wd, i) => (
          <div
            key={wd}
            className="py-1 text-center text-[11px] font-medium"
            style={{
              color:
                i === 0
                  ? 'var(--color-sunday)'
                  : i === 6
                    ? 'var(--color-saturday)'
                    : 'var(--color-text-tertiary)',
            }}
          >
            {wd}
          </div>
        ))}
      </div>

      {/* Date grid */}
      <div className="grid grid-cols-7 gap-0">
        {emptySlots > 0 && <div style={{ gridColumn: `span ${emptySlots}` }} />}
        {calendarDays.map((day) => {
          const isSelected = day.hasSame(value, 'day');
          const isToday = day.hasSame(DateTime.now(), 'day');
          const isDisabled = minDate ? day < minDate.startOf('day') : false;
          const dayOfWeek = day.weekday % 7;
          return (
            <button
              key={day.toISODate()}
              type="button"
              onClick={() => handleSelect(day)}
              disabled={isDisabled}
              className="flex h-9 w-full items-center justify-center rounded-full text-[13px] transition-colors"
              style={{
                backgroundColor: isSelected ? 'var(--color-accent)' : 'transparent',
                color: isSelected
                  ? '#ffffff'
                  : isDisabled
                    ? 'var(--color-text-tertiary)'
                    : dayOfWeek === 0
                      ? 'var(--color-sunday)'
                      : dayOfWeek === 6
                        ? 'var(--color-saturday)'
                        : 'var(--color-text-primary)',
                fontWeight: isToday ? 700 : isSelected ? 600 : 400,
                opacity: isDisabled ? 0.3 : 1,
                outline: isToday && !isSelected ? '2px solid var(--color-accent)' : 'none',
                outlineOffset: '-2px',
              }}
            >
              {day.day}
            </button>
          );
        })}
      </div>
    </div>,
    document.body,
  );
}

/* ============================================
   TimePicker — scrollable time slot dropdown
   ============================================ */

interface TimePickerDropdownProps {
  value: string;
  onChange: (time: string) => void;
  onClose: () => void;
  style: React.CSSProperties;
  floatingRef: React.RefObject<HTMLDivElement | null>;
}

const TIME_SLOTS = (() => {
  const slots: string[] = [];
  for (let h = 0; h < 24; h++) {
    for (let m = 0; m < 60; m += 15) {
      slots.push(`${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`);
    }
  }
  return slots;
})();

function TimePickerDropdown({
  value,
  onChange,
  onClose,
  style,
  floatingRef,
}: TimePickerDropdownProps) {
  const selectedRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (floatingRef.current && !floatingRef.current.contains(e.target as Node)) onClose();
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose, floatingRef]);

  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'center' });
  }, []);

  const nearestSlot = useMemo(() => {
    const [h, m] = value.split(':').map(Number);
    const totalMin = (h ?? 0) * 60 + (m ?? 0);
    const rounded = Math.round(totalMin / 15) * 15;
    const rh = Math.floor(rounded / 60) % 24;
    const rm = rounded % 60;
    return `${String(rh).padStart(2, '0')}:${String(rm).padStart(2, '0')}`;
  }, [value]);

  return createPortal(
    <div
      ref={floatingRef}
      className="fixed z-[9999] max-h-[240px] w-[100px] overflow-y-auto rounded-xl bg-[var(--color-surface-elevated)] py-1 ring-1 ring-[var(--color-border)]"
      style={{ ...style, boxShadow: 'var(--shadow-elevated)', backdropFilter: 'blur(20px)' }}
    >
      {TIME_SLOTS.map((slot) => {
        const isSelected = slot === nearestSlot;
        return (
          <button
            key={slot}
            ref={isSelected ? selectedRef : undefined}
            type="button"
            onClick={() => {
              onChange(slot);
              onClose();
            }}
            className="flex w-full items-center justify-center py-2 text-[14px] transition-colors"
            style={{
              backgroundColor: isSelected ? 'var(--color-accent-bg)' : 'transparent',
              color: isSelected ? 'var(--color-accent)' : 'var(--color-text-primary)',
              fontWeight: isSelected ? 600 : 400,
            }}
          >
            {slot}
          </button>
        );
      })}
    </div>,
    document.body,
  );
}

/* ============================================
   CustomSelect — styled dropdown replacement
   ============================================ */

interface SelectOption {
  value: string;
  label: string;
}

interface CustomSelectProps {
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  className?: string;
}

export function CustomSelect({ value, options, onChange, className }: CustomSelectProps) {
  const [open, setOpen] = useState(false);
  const { anchorRef, floatingRef, pos } = useFloating(open);
  const selectedRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (
        floatingRef.current &&
        !floatingRef.current.contains(e.target as Node) &&
        anchorRef.current &&
        !anchorRef.current.contains(e.target as Node)
      ) {
        setOpen(false);
      }
    };
    if (open) {
      document.addEventListener('mousedown', handler);
      setTimeout(() => selectedRef.current?.scrollIntoView({ block: 'center' }), 0);
    }
    return () => document.removeEventListener('mousedown', handler);
  }, [open, floatingRef, anchorRef]);

  const selected = options.find((o) => o.value === value);

  return (
    <div className={`relative ${className ?? ''}`}>
      <button
        ref={anchorRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-2 rounded-lg bg-[var(--color-surface-inset)] py-1.5 pr-3 pl-3 text-[14px] text-[var(--color-text-primary)] transition-colors hover:bg-[var(--color-hover)]"
      >
        <span className="truncate">{selected?.label ?? value}</span>
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--color-text-tertiary)"
          strokeWidth="2"
          style={{
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 0.15s ease',
          }}
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>

      {open &&
        createPortal(
          <div
            ref={floatingRef}
            className="fixed z-[9999] max-h-[240px] min-w-[160px] overflow-y-auto rounded-xl bg-[var(--color-surface-elevated)] py-1 ring-1 ring-[var(--color-border)]"
            style={{
              top: pos.top,
              left: pos.left,
              boxShadow: 'var(--shadow-elevated)',
              backdropFilter: 'blur(20px)',
            }}
          >
            {options.map((opt) => {
              const isSelected = opt.value === value;
              return (
                <button
                  key={opt.value}
                  ref={isSelected ? selectedRef : undefined}
                  type="button"
                  onClick={() => {
                    onChange(opt.value);
                    setOpen(false);
                  }}
                  className="flex w-full items-center gap-2 whitespace-nowrap px-3 py-2.5 text-left text-[14px] transition-colors"
                  style={{
                    backgroundColor: isSelected ? 'var(--color-accent-bg)' : 'transparent',
                    color: isSelected ? 'var(--color-accent)' : 'var(--color-text-primary)',
                    fontWeight: isSelected ? 600 : 400,
                  }}
                >
                  {isSelected && (
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="var(--color-accent)"
                      strokeWidth="2.5"
                    >
                      <path d="M20 6L9 17l-5-5" />
                    </svg>
                  )}
                  <span className={isSelected ? '' : 'pl-[22px]'}>{opt.label}</span>
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </div>
  );
}

/* ============================================
   DateTimeField — tappable date/time display
   that opens DatePicker and TimePicker
   ============================================ */

interface DateTimeFieldProps {
  label: string;
  dateValue: DateTime;
  timeValue: string; // "HH:mm"
  showTime: boolean;
  onDateChange: (date: DateTime) => void;
  onTimeChange: (time: string) => void;
  minDate?: DateTime;
}

export function DateTimeField({
  label,
  dateValue,
  timeValue,
  showTime,
  onDateChange,
  onTimeChange,
  minDate,
}: DateTimeFieldProps) {
  const locale = useUiStore((s) => s.locale);
  const [showDatePicker, setShowDatePicker] = useState(false);
  const [showTimePicker, setShowTimePicker] = useState(false);
  const dateFloating = useFloating(showDatePicker);
  const timeFloating = useFloating(showTimePicker);

  const dateLabel = useMemo(() => {
    if (locale === 'en') return dateValue.toFormat('EEE, MMM d, yyyy');
    const dow = ['日', '月', '火', '水', '木', '金', '土'][dateValue.weekday % 7] ?? '';
    return `${dateValue.year}年${dateValue.month}月${dateValue.day}日(${dow})`;
  }, [dateValue, locale]);

  const handleCloseDatePicker = useCallback(() => setShowDatePicker(false), []);
  const handleCloseTimePicker = useCallback(() => setShowTimePicker(false), []);

  return (
    <div className="flex items-center justify-between py-2.5">
      {label && <span className="text-[14px] text-[var(--color-text-secondary)]">{label}</span>}
      <div className="flex items-center gap-2">
        <button
          ref={dateFloating.anchorRef}
          type="button"
          onClick={() => {
            setShowDatePicker((v) => !v);
            setShowTimePicker(false);
          }}
          className="rounded-lg bg-[var(--color-accent-bg)] px-3 py-1.5 text-[14px] font-medium text-[var(--color-accent)] transition-colors hover:bg-[var(--color-accent-subtle)]"
        >
          {dateLabel}
        </button>
        {showDatePicker && (
          <DatePickerDropdown
            value={dateValue}
            onChange={onDateChange}
            onClose={handleCloseDatePicker}
            {...(minDate ? { minDate } : {})}
            style={{ top: dateFloating.pos.top, left: dateFloating.pos.left }}
            floatingRef={dateFloating.floatingRef}
          />
        )}
        {showTime && (
          <>
            <button
              ref={timeFloating.anchorRef}
              type="button"
              onClick={() => {
                setShowTimePicker((v) => !v);
                setShowDatePicker(false);
              }}
              className="rounded-lg bg-[var(--color-accent-bg)] px-3 py-1.5 text-[14px] font-medium text-[var(--color-accent)] transition-colors hover:bg-[var(--color-accent-subtle)]"
            >
              {timeValue}
            </button>
            {showTimePicker && (
              <TimePickerDropdown
                value={timeValue}
                onChange={onTimeChange}
                onClose={handleCloseTimePicker}
                style={{ top: timeFloating.pos.top, left: timeFloating.pos.left }}
                floatingRef={timeFloating.floatingRef}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
