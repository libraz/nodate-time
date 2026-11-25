import { DateTime } from 'luxon';
import { useCallback, useMemo, useState } from 'react';
import { DatePickerDropdown, type DateRangeLink } from '@/components/date-picker';
import { useFloating } from '@/components/picker-surface';
import { TimePickerDropdown } from '@/components/time-picker';
import { type Locale, useT } from '@/i18n';
import { getWeekdayLabel } from '@/lib/date-utils';
import { useUiStore } from '@/stores/ui-store';

/* ============================================
   DateTimeField — tappable date/time display
   that opens DatePicker and TimePicker
   ============================================ */

/** Shared look for a form field that opens a picker panel, matching `input-modern input-compact`. */
const FORM_PICKER_TRIGGER =
  'input-modern input-compact flex w-full items-center justify-between gap-2 text-left tabular-nums text-[var(--color-text-primary)]';

function PickerFormLabel({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 text-footnote font-medium text-[var(--color-text-secondary)]">
      <span className="mb-1.5 block">{label}</span>
      {children}
    </div>
  );
}

function TriggerIcon({ path }: { path: string }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className="shrink-0 text-[var(--color-text-tertiary)]"
    >
      <path d={path} />
    </svg>
  );
}

const CALENDAR_ICON =
  'M7 3v3m10-3v3M4 9h16M5 5h14a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1z';
const CLOCK_ICON = 'M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z';

/** A labelled form date field; the year is shown only when it is not the current one. */
export function DateField({
  label,
  value,
  onChange,
  minDate,
  range,
}: {
  label: string;
  value: DateTime;
  onChange: (date: DateTime) => void;
  minDate?: DateTime;
  range?: DateRangeLink;
}) {
  const locale = useUiStore((s) => s.locale);
  const [open, setOpen] = useState(false);
  const floating = useFloating(open, 280);
  const close = useCallback(() => setOpen(false), []);
  const dow = getWeekdayLabel(value.weekday % 7, locale);
  const sameYear = value.year === DateTime.now().setZone(value.zone).year;
  const text =
    locale === 'en'
      ? value.setLocale('en').toFormat(sameYear ? 'EEE, MMM d' : 'MMM d, yyyy')
      : `${sameYear ? '' : `${value.year}年`}${value.month}月${value.day}日(${dow})`;
  return (
    <PickerFormLabel label={label}>
      <button
        ref={floating.anchorRef}
        type="button"
        aria-label={`${label} ${value.toISODate()}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={FORM_PICKER_TRIGGER}
      >
        <span className="truncate">{text}</span>
        <TriggerIcon path={CALENDAR_ICON} />
      </button>
      {open && (
        <DatePickerDropdown
          value={value}
          onChange={onChange}
          onClose={close}
          {...(minDate ? { minDate } : {})}
          {...(range ? { range } : {})}
          title={label}
          style={{ top: floating.pos.top, left: floating.pos.left }}
          floatingRef={floating.floatingRef}
          returnFocusRef={floating.anchorRef}
        />
      )}
    </PickerFormLabel>
  );
}

/** A labelled form time field (`HH:mm`) that opens the 15-minute slot list. */
export function TimeField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (time: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const floating = useFloating(open, 232);
  const close = useCallback(() => setOpen(false), []);
  return (
    <PickerFormLabel label={label}>
      <button
        ref={floating.anchorRef}
        type="button"
        aria-label={`${label} ${value}`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={FORM_PICKER_TRIGGER}
      >
        <span className="truncate">{value}</span>
        <TriggerIcon path={CLOCK_ICON} />
      </button>
      {open && (
        <TimePickerDropdown
          value={value}
          onChange={onChange}
          onClose={close}
          title={label}
          style={{
            top: floating.pos.top,
            left: floating.pos.left,
            width: Math.max(120, floating.pos.width),
          }}
          floatingRef={floating.floatingRef}
          returnFocusRef={floating.anchorRef}
        />
      )}
    </PickerFormLabel>
  );
}

/** A picker field's date: weekday included, so the day is checkable at a glance. */
function formatFieldDate(date: DateTime, locale: Locale): string {
  if (locale === 'en') return date.setLocale('en').toFormat('EEE, MMM d, yyyy');
  const dow = getWeekdayLabel(date.weekday % 7, locale);
  return `${date.year}年${date.month}月${date.day}日(${dow})`;
}

/** The same date split for a two-tier display: the year as a caption over the day. */
function formatFieldDateParts(date: DateTime, locale: Locale): { year: string; day: string } {
  if (locale === 'en') {
    const d = date.setLocale('en');
    return { year: d.toFormat('yyyy'), day: d.toFormat('EEE, MMM d') };
  }
  const dow = getWeekdayLabel(date.weekday % 7, locale);
  return { year: `${date.year}年`, day: `${date.month}月${date.day}日(${dow})` };
}

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
  const dateFloating = useFloating(showDatePicker, 280);
  const timeFloating = useFloating(showTimePicker, 232);

  const dateLabel = useMemo(() => formatFieldDate(dateValue, locale), [dateValue, locale]);

  const handleCloseDatePicker = useCallback(() => setShowDatePicker(false), []);
  const handleCloseTimePicker = useCallback(() => setShowTimePicker(false), []);

  return (
    <div className="flex items-center justify-between py-2.5">
      {label && <span className="text-default text-[var(--color-text-primary)]">{label}</span>}
      <div className="flex items-center gap-2">
        <button
          ref={dateFloating.anchorRef}
          type="button"
          onClick={() => {
            setShowDatePicker((v) => !v);
            setShowTimePicker(false);
          }}
          className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent-ink)] hover:bg-[var(--color-accent-subtle)]"
        >
          {dateLabel}
        </button>
        {showDatePicker && (
          <DatePickerDropdown
            value={dateValue}
            onChange={onDateChange}
            onClose={handleCloseDatePicker}
            {...(minDate ? { minDate } : {})}
            {...(label ? { title: label } : {})}
            style={{ top: dateFloating.pos.top, left: dateFloating.pos.left }}
            floatingRef={dateFloating.floatingRef}
            returnFocusRef={dateFloating.anchorRef}
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
              className="pill-button bg-[var(--color-accent-bg)] px-3 py-1.5 text-default font-medium tabular-nums text-[var(--color-accent-ink)] hover:bg-[var(--color-accent-subtle)]"
            >
              {timeValue}
            </button>
            {showTimePicker && (
              <TimePickerDropdown
                value={timeValue}
                onChange={onTimeChange}
                onClose={handleCloseTimePicker}
                {...(label ? { title: label } : {})}
                style={{ top: timeFloating.pos.top, left: timeFloating.pos.left }}
                floatingRef={timeFloating.floatingRef}
                returnFocusRef={timeFloating.anchorRef}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}

interface RangeEndProps {
  label: string;
  date: DateTime;
  time: string; // "HH:mm"
  showTime: boolean;
  onDateChange: (date: DateTime) => void;
  onTimeChange: (time: string) => void;
  range: DateRangeLink;
  minDate?: DateTime;
  /** Draws the value as the error the caller reports. */
  invalid?: boolean;
}

const RANGE_HIT =
  '-mx-1.5 rounded-[var(--radius-sm)] px-1.5 text-left tabular-nums transition-colors enabled:hover:bg-[var(--color-hover)] focus-visible:outline-2 focus-visible:outline-[var(--color-accent)]';
const RANGE_OPEN = 'bg-[var(--color-accent-bg)] text-[var(--color-accent-ink)]';

/** One end of a range: the date as a caption above the time, each its own picker. */
function RangeEnd({
  label,
  date,
  time,
  showTime,
  onDateChange,
  onTimeChange,
  range,
  minDate,
  invalid = false,
}: RangeEndProps) {
  const t = useT();
  const locale = useUiStore((s) => s.locale);
  const [picker, setPicker] = useState<'date' | 'time' | null>(null);
  const dateFloating = useFloating(picker === 'date', 280);
  const timeFloating = useFloating(picker === 'time', 232);
  const close = useCallback(() => setPicker(null), []);
  const dateLabel = useMemo(() => formatFieldDate(date, locale), [date, locale]);
  const dateParts = useMemo(() => formatFieldDateParts(date, locale), [date, locale]);
  const start = range.role === 'start';
  // An end on the start's day repeats nothing the reader needs; the label still names it.
  const sameDay = !start && date.hasSame(range.other, 'day');
  const tone = invalid
    ? 'text-[var(--color-danger)]'
    : start
      ? 'text-[var(--color-text-primary)]'
      : 'text-[var(--color-text-secondary)]';
  const weight = start ? 'font-semibold' : 'font-normal';

  return (
    <div className="flex min-w-0 flex-col items-start">
      <button
        ref={dateFloating.anchorRef}
        type="button"
        aria-label={`${label} ${dateLabel}`}
        aria-expanded={picker === 'date'}
        onClick={() => setPicker((p) => (p === 'date' ? null : 'date'))}
        className={`${RANGE_HIT} flex max-w-full flex-col items-start py-0.5 ${weight} ${
          picker === 'date' ? RANGE_OPEN : tone
        }`}
      >
        {showTime ? (
          <span className="max-w-full truncate text-footnote">
            {sameDay ? t('event.sameDay') : dateLabel}
          </span>
        ) : (
          <>
            {/* A blank caption keeps the day on the start's baseline. */}
            <span className="text-footnote">{sameDay ? '\u00a0' : dateParts.year}</span>
            <span className="max-w-full truncate text-title leading-tight">
              {sameDay ? t('event.sameDay') : dateParts.day}
            </span>
          </>
        )}
      </button>
      {picker === 'date' && (
        <DatePickerDropdown
          value={date}
          onChange={onDateChange}
          onClose={close}
          range={range}
          {...(minDate ? { minDate } : {})}
          title={label}
          style={{ top: dateFloating.pos.top, left: dateFloating.pos.left }}
          floatingRef={dateFloating.floatingRef}
          returnFocusRef={dateFloating.anchorRef}
        />
      )}
      {showTime && (
        <button
          ref={timeFloating.anchorRef}
          type="button"
          aria-label={`${label} ${time}`}
          aria-expanded={picker === 'time'}
          onClick={() => setPicker((p) => (p === 'time' ? null : 'time'))}
          className={`${RANGE_HIT} text-hero leading-tight ${weight} ${
            picker === 'time' ? RANGE_OPEN : tone
          }`}
        >
          {time}
        </button>
      )}
      {picker === 'time' && (
        <TimePickerDropdown
          value={time}
          onChange={onTimeChange}
          onClose={close}
          title={label}
          style={{ top: timeFloating.pos.top, left: timeFloating.pos.left }}
          floatingRef={timeFloating.floatingRef}
          returnFocusRef={timeFloating.anchorRef}
        />
      )}
    </div>
  );
}

interface RangeEndValue {
  label: string;
  date: DateTime;
  time: string; // "HH:mm"
  onDateChange: (date: DateTime) => void;
  onTimeChange: (time: string) => void;
}

interface DateTimeRangeFieldProps {
  start: RangeEndValue;
  end: RangeEndValue;
  showTime: boolean;
  /** Draws the end as the error the caller reports below the field. */
  endInvalid?: boolean;
}

/**
 * An event's span read left to right: start, a chevron, end. The start carries
 * the weight because it is what a reader looks for first; each date and time
 * opens its own picker.
 */
export function DateTimeRangeField({
  start,
  end,
  showTime,
  endInvalid = false,
}: DateTimeRangeFieldProps) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] items-center gap-x-2 py-2">
      <RangeEnd
        label={start.label}
        date={start.date}
        time={start.time}
        showTime={showTime}
        onDateChange={start.onDateChange}
        onTimeChange={start.onTimeChange}
        range={{ role: 'start', other: end.date }}
      />
      <svg
        width="20"
        height="20"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        className="mt-5 text-[var(--color-text-tertiary)]"
      >
        <path d="M9 18l6-6-6-6" />
      </svg>
      <RangeEnd
        label={end.label}
        date={end.date}
        time={end.time}
        showTime={showTime}
        onDateChange={end.onDateChange}
        onTimeChange={end.onTimeChange}
        range={{ role: 'end', other: start.date }}
        minDate={start.date}
        invalid={endInvalid}
      />
    </div>
  );
}
