import { DateTime } from 'luxon';
import { type InputHTMLAttributes, useEffect, useRef, useState } from 'react';
import { formatSchedulePollSlotTime, slotTimeRange } from '@/components/schedule-poll-ui';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/client';
import {
  type SchedulePollAvailabilityInput,
  type SchedulePollCandidate,
  schedulePollApi,
} from '@/lib/schedule-polls';
import { toast } from '@/lib/toast';
import { useUiStore } from '@/stores/ui-store';

const candidateKey = (candidate: Pick<SchedulePollCandidate, 'startAt' | 'endAt'>) =>
  `${Date.parse(candidate.startAt)}/${Date.parse(candidate.endAt)}`;

export function SchedulePollCandidateFinder({
  calendarId,
  memberIds,
  selected,
  onChange,
}: {
  calendarId: string;
  memberIds: string[];
  selected: SchedulePollCandidate[];
  onChange: (slots: SchedulePollCandidate[]) => void;
}) {
  const t = useT();
  const timezone = useUiStore((state) => state.timezone);
  const memberKey = memberIds.join('|');
  const requestIdentity = `${calendarId}\u0000${memberKey}`;
  const memberKeyRef = useRef(memberKey);
  const previousIdentity = useRef(requestIdentity);
  const requestSequence = useRef(0);
  const activeController = useRef<AbortController | null>(null);
  const selectedRef = useRef(selected);
  const onChangeRef = useRef(onChange);
  memberKeyRef.current = memberKey;
  selectedRef.current = selected;
  onChangeRef.current = onChange;
  const today = DateTime.now().setZone(timezone).toISODate() ?? '';
  const [fromDate, setFromDate] = useState(today);
  const [toDate, setToDate] = useState(
    DateTime.now().setZone(timezone).plus({ days: 14 }).toISODate() ?? today,
  );
  const [startTime, setStartTime] = useState('09:00');
  const [endTime, setEndTime] = useState('18:00');
  const [durationMinutes, setDurationMinutes] = useState(60);
  const [manualStartDate, setManualStartDate] = useState(today);
  const [manualStartTime, setManualStartTime] = useState('10:00');
  const [manualEndDate, setManualEndDate] = useState(today);
  const [manualEndTime, setManualEndTime] = useState('11:00');
  const [suggestions, setSuggestions] = useState<SchedulePollCandidate[]>([]);
  const [searching, setSearching] = useState(false);
  const [searched, setSearched] = useState(false);
  // Grid rows are ordered from the searched window's start, so an overnight window reads top-down.
  const [searchedStartTime, setSearchedStartTime] = useState(startTime);

  useEffect(() => {
    if (previousIdentity.current !== requestIdentity) {
      previousIdentity.current = requestIdentity;
      activeController.current?.abort();
      requestSequence.current += 1;
      setSuggestions([]);
      setSearched(false);
      setSearching(false);
    }
    return () => {
      activeController.current?.abort();
      requestSequence.current += 1;
    };
  }, [requestIdentity]);

  const runSearch = async (input: SchedulePollAvailabilityInput, mode: 'replace' | 'append') => {
    const sequence = ++requestSequence.current;
    const currentMemberKey = memberKey;
    activeController.current?.abort();
    const controller = new AbortController();
    activeController.current = controller;
    setSearching(true);
    setSearched(true);
    try {
      const result = await schedulePollApi.availability(calendarId, input, controller.signal);
      if (sequence !== requestSequence.current || currentMemberKey !== memberKeyRef.current) return;
      const replacingKeys = new Set((input.slots ?? []).map(candidateKey));
      const merged = new Map(
        (mode === 'append'
          ? suggestions.filter((candidate) => !replacingKeys.has(candidateKey(candidate)))
          : []
        ).map((candidate) => [candidateKey(candidate), candidate]),
      );
      for (const candidate of result) merged.set(candidateKey(candidate), candidate);
      const nextSuggestions = [...merged.values()];
      setSuggestions(nextSuggestions);
      const byKey = new Map(
        nextSuggestions.map((candidate) => [candidateKey(candidate), candidate]),
      );
      const priorSelection = selectedRef.current;
      onChangeRef.current(
        priorSelection
          .map((slot) => byKey.get(candidateKey(slot)))
          .filter(
            (slot): slot is SchedulePollCandidate =>
              !!slot && slot.active && slot.availability !== 'hard',
          ),
      );
    } catch (error) {
      if (!controller.signal.aborted && sequence === requestSequence.current)
        toast.error(errorMessage(error));
    } finally {
      if (sequence === requestSequence.current) setSearching(false);
    }
  };

  const findCandidates = async () => {
    if (!calendarId || memberIds.length === 0 || !fromDate || !toDate) return;
    setSearchedStartTime(startTime);
    void runSearch(
      {
        timezone,
        fromDate,
        toDate,
        startTime,
        endTime,
        durationMinutes,
        memberIds,
      },
      'replace',
    );
  };

  const addManualCandidate = () => {
    const startLocal = `${manualStartDate}T${manualStartTime}`;
    const endLocal = `${manualEndDate}T${manualEndTime}`;
    const start = DateTime.fromISO(startLocal, { zone: timezone });
    const end = DateTime.fromISO(endLocal, { zone: timezone });
    const duration = end.diff(start, 'minutes').minutes;
    if (
      !start.isValid ||
      !end.isValid ||
      start.toFormat("yyyy-MM-dd'T'HH:mm") !== startLocal ||
      end.toFormat("yyyy-MM-dd'T'HH:mm") !== endLocal ||
      duration <= 0 ||
      duration > 1440 ||
      !Number.isInteger(duration)
    ) {
      toast.error(t('schedulePoll.invalidManualCandidate'));
      return;
    }
    const startAt = start.toISO({ includeOffset: true });
    const endAt = end.toISO({ includeOffset: true });
    if (!startAt || !endAt) {
      toast.error(t('schedulePoll.invalidManualCandidate'));
      return;
    }
    void runSearch(
      {
        timezone,
        fromDate: manualStartDate,
        toDate: manualEndDate,
        startTime: manualStartTime,
        endTime: manualEndTime,
        durationMinutes: duration,
        memberIds,
        slots: [{ startAt, endAt }],
      },
      'append',
    );
  };

  const selectedKeys = new Set(selected.map(candidateKey));
  const toggle = (candidate: SchedulePollCandidate) => {
    const key = candidateKey(candidate);
    if (selectedKeys.has(key)) {
      onChange(selected.filter((slot) => candidateKey(slot) !== key));
    } else {
      onChange([...selected, candidate]);
    }
  };

  return (
    <div className="page-card p-4 sm:p-5">
      <div className="mb-4">
        <h3 className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('schedulePoll.findCandidates')}
        </h3>
        <p className="mt-0.5 text-footnote text-[var(--color-text-secondary)]">
          {t('schedulePoll.findCandidatesHint')}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-[repeat(4,minmax(0,1fr))_minmax(0,0.8fr)]">
        <Field
          label={t('schedulePoll.fromDate')}
          type="date"
          value={fromDate}
          onChange={(event) => setFromDate(event.target.value)}
        />
        <Field
          label={t('schedulePoll.toDate')}
          type="date"
          value={toDate}
          onChange={(event) => setToDate(event.target.value)}
        />
        <Field
          label={t('schedulePoll.startTime')}
          type="time"
          value={startTime}
          onChange={(event) => setStartTime(event.target.value)}
        />
        <Field
          label={t('schedulePoll.endTime')}
          type="time"
          value={endTime}
          onChange={(event) => setEndTime(event.target.value)}
        />
        <div className="col-span-2 sm:col-span-1">
          <Field
            label={t('schedulePoll.durationMinutes')}
            type="number"
            min={15}
            max={1440}
            step={15}
            value={durationMinutes}
            onChange={(event) => setDurationMinutes(Number(event.target.value))}
          />
        </div>
      </div>
      <div className="mt-3 flex justify-end">
        <button
          type="button"
          onClick={findCandidates}
          disabled={
            searching || memberIds.length === 0 || fromDate > toDate || durationMinutes < 15
          }
          className="btn-primary inline-flex h-9 items-center gap-2 px-4 text-body"
        >
          {searching ? (
            <span aria-hidden className="spinner h-3.5 w-3.5" />
          ) : (
            <svg
              aria-hidden
              width="15"
              height="15"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
            >
              <circle cx="7" cy="7" r="4.5" />
              <path d="m10.5 10.5 3 3" />
            </svg>
          )}
          {t('schedulePoll.findCandidates')}
        </button>
      </div>

      {searched && (
        <div className="mt-5 border-t border-[var(--color-separator)] pt-4">
          {suggestions.length === 0 && !searching ? (
            <p className="py-2 text-body text-[var(--color-text-secondary)]">
              {t('schedulePoll.noCandidates')}
            </p>
          ) : (
            <SlotGrid
              suggestions={suggestions}
              selected={selected}
              selectedKeys={selectedKeys}
              anchorTime={searchedStartTime}
              disabled={searching}
              onToggle={toggle}
            />
          )}
        </div>
      )}

      <details className="group mt-4 border-t border-[var(--color-separator)] pt-3">
        <summary className="flex cursor-pointer list-none items-center gap-2 py-1 text-footnote font-semibold text-[var(--color-text-secondary)] hover:text-[var(--color-text-primary)] [&::-webkit-details-marker]:hidden">
          <svg
            aria-hidden
            width="12"
            height="12"
            viewBox="0 0 12 12"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            className="transition-transform group-open:rotate-90"
          >
            <path d="m4.5 2.5 3.5 3.5-3.5 3.5" />
          </svg>
          {t('schedulePoll.manualCandidate')}
        </summary>
        <p className="mb-3 mt-1 text-footnote text-[var(--color-text-tertiary)]">
          {t('schedulePoll.manualCandidateHint', { timezone })}
        </p>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-[repeat(4,minmax(0,1fr))_auto] sm:items-end">
          <Field
            label={t('schedulePoll.manualStartDate')}
            type="date"
            value={manualStartDate}
            onChange={(event) => setManualStartDate(event.target.value)}
          />
          <Field
            label={t('schedulePoll.manualStartTime')}
            type="time"
            value={manualStartTime}
            onChange={(event) => setManualStartTime(event.target.value)}
          />
          <Field
            label={t('schedulePoll.manualEndDate')}
            type="date"
            value={manualEndDate}
            onChange={(event) => setManualEndDate(event.target.value)}
          />
          <Field
            label={t('schedulePoll.manualEndTime')}
            type="time"
            value={manualEndTime}
            onChange={(event) => setManualEndTime(event.target.value)}
          />
          <button
            type="button"
            onClick={addManualCandidate}
            disabled={searching || memberIds.length === 0}
            className="btn-secondary col-span-2 h-[34px] px-4 text-body sm:col-span-1"
          >
            {t('schedulePoll.addManualCandidate')}
          </button>
        </div>
      </details>
    </div>
  );
}

type GridEntry = {
  candidate: SchedulePollCandidate;
  key: string;
  start: DateTime;
  end: DateTime;
};

const minuteOfDay = (time: DateTime) => time.hour * 60 + time.minute;
const durationOf = (entry: GridEntry) => Math.round(entry.end.diff(entry.start, 'minutes').minutes);

/**
 * Candidates as a day-by-start-time grid: one column per day, one row per start time, so the
 * same time lines up across days and a time taken by someone's plans shows as a hatched gap.
 */
function SlotGrid({
  suggestions,
  selected,
  selectedKeys,
  anchorTime,
  disabled,
  onToggle,
}: {
  suggestions: SchedulePollCandidate[];
  selected: SchedulePollCandidate[];
  selectedKeys: Set<string>;
  anchorTime: string;
  disabled: boolean;
  onToggle: (candidate: SchedulePollCandidate) => void;
}) {
  const t = useT();
  const timezone = useUiStore((state) => state.timezone);
  const locale = useUiStore((state) => state.locale);
  const toLocal = (iso: string) =>
    DateTime.fromISO(iso, { setZone: true }).setZone(timezone).setLocale(locale);

  const entries: GridEntry[] = suggestions.map((candidate) => ({
    candidate,
    key: candidateKey(candidate),
    start: toLocal(candidate.startAt),
    end: toLocal(candidate.endAt),
  }));
  const [anchorHour = 0, anchorMinute = 0] = anchorTime.split(':').map(Number);
  const anchor = anchorHour * 60 + anchorMinute;
  const rowOrder = (minute: number) => (minute - anchor + 1440) % 1440;
  const rows = [...new Set(entries.map((entry) => minuteOfDay(entry.start)))].sort(
    (a, b) => rowOrder(a) - rowOrder(b),
  );
  const days = [...new Set(entries.map((entry) => entry.start.toISODate() ?? ''))].sort();
  const cells = new Map<string, GridEntry[]>();
  const durations = new Map<number, number>();
  for (const entry of entries) {
    const cellKey = `${entry.start.toISODate()}|${minuteOfDay(entry.start)}`;
    const cell = cells.get(cellKey);
    if (cell) cell.push(entry);
    else cells.set(cellKey, [entry]);
    durations.set(durationOf(entry), (durations.get(durationOf(entry)) ?? 0) + 1);
  }
  const usualDuration = [...durations.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] ?? 0;
  const selectedEntries = selected
    .map((candidate) => ({
      candidate,
      start: toLocal(candidate.startAt),
      end: toLocal(candidate.endAt),
    }))
    .sort((a, b) => a.start.toMillis() - b.start.toMillis());

  return (
    <div>
      <div className="mb-2.5 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <p className="text-footnote text-[var(--color-text-secondary)]">
          {t('schedulePoll.slotGridHint', { minutes: usualDuration })}
        </p>
        <ul className="flex items-center gap-3 text-micro text-[var(--color-text-tertiary)]">
          <li className="flex items-center gap-1.5">
            <span aria-hidden className="slot-grid-cell h-3 w-4" data-availability="open" />
            {t('schedulePoll.availabilityOpen')}
          </li>
          <li className="flex items-center gap-1.5">
            <span aria-hidden className="slot-grid-cell h-3 w-4" data-availability="soft" />
            {t('schedulePoll.availabilitySoft')}
          </li>
          <li className="flex items-center gap-1.5">
            <span aria-hidden className="slot-grid-busy h-3 w-4 rounded-[3px]" />
            {t('schedulePoll.availabilityHard')}
          </li>
        </ul>
      </div>

      <div className="slot-grid-panel max-h-[460px] overflow-auto rounded-[var(--radius-control)] border border-[var(--color-separator)]">
        <div
          className="grid min-w-max pb-2"
          style={{ gridTemplateColumns: `3.25rem repeat(${days.length}, minmax(4.5rem, 1fr))` }}
        >
          <div className="slot-grid-sticky sticky top-0 left-0 z-20 border-b border-[var(--color-separator)]" />
          {days.map((day) => {
            const date = DateTime.fromISO(day, { zone: timezone }).setLocale(locale);
            const weekdayColor =
              date.weekday === 7
                ? 'text-[var(--color-sunday)]'
                : date.weekday === 6
                  ? 'text-[var(--color-saturday)]'
                  : 'text-[var(--color-text-tertiary)]';
            return (
              <div
                key={day}
                className="slot-grid-sticky sticky top-0 z-10 flex items-baseline justify-center gap-1 border-b border-[var(--color-separator)] py-2 tabular-nums"
              >
                <span className="text-default font-semibold text-[var(--color-text-primary)]">
                  {date.toFormat('M/d')}
                </span>
                <span className={`text-micro font-medium ${weekdayColor}`}>
                  {date.toFormat('ccc')}
                </span>
              </div>
            );
          })}
          {rows.map((minute, rowIndex) => {
            const onHour = minute % 60 === 0;
            const rowSpacing =
              rowIndex === 0
                ? 'pt-2'
                : onHour
                  ? 'mt-1 border-t border-[var(--color-separator)] pt-1.5'
                  : 'pt-0.5';
            const time = DateTime.fromObject({ hour: Math.floor(minute / 60), minute: minute % 60 })
              .setLocale(locale)
              .toLocaleString(DateTime.TIME_SIMPLE);
            return [
              <div
                key={`axis|${minute}`}
                className={`slot-grid-sticky sticky left-0 z-[5] flex items-start justify-end pr-2 tabular-nums ${rowSpacing}`}
              >
                <span
                  className={`leading-6 ${onHour ? 'text-footnote font-semibold text-[var(--color-text-secondary)]' : 'text-micro text-[var(--color-text-tertiary)]'}`}
                >
                  {time}
                </span>
              </div>,
              ...days.map((day) => {
                const cell = cells.get(`${day}|${minute}`);
                return (
                  <div
                    key={`${day}|${minute}`}
                    className={`flex flex-col gap-0.5 px-0.5 ${rowSpacing}`}
                  >
                    {cell ? (
                      cell.map((entry) => (
                        <SlotCell
                          key={entry.key}
                          entry={entry}
                          showEnd={
                            durationOf(entry) !== usualDuration ||
                            !entry.start.hasSame(entry.end, 'day')
                          }
                          checked={selectedKeys.has(entry.key)}
                          disabled={disabled}
                          onToggle={onToggle}
                        />
                      ))
                    ) : (
                      <span
                        aria-hidden
                        className="slot-grid-busy h-6 rounded-[var(--radius-control)]"
                      />
                    )}
                  </div>
                );
              }),
            ];
          })}
        </div>
      </div>

      <div className="mt-3">
        <p
          role="status"
          className={`text-footnote font-semibold tabular-nums ${selected.length > 0 ? 'text-[var(--color-accent-ink)]' : 'text-[var(--color-text-tertiary)]'}`}
        >
          {t('schedulePoll.selectedCandidates', { count: selected.length })}
        </p>
        {selectedEntries.length > 0 && (
          <ul className="mt-2 flex flex-wrap gap-1.5">
            {selectedEntries.map(({ candidate, start, end }) => (
              <li key={candidateKey(candidate)}>
                <button
                  type="button"
                  onClick={() => onToggle(candidate)}
                  disabled={disabled}
                  aria-label={`${formatSchedulePollSlotTime(candidate.startAt, candidate.endAt, timezone, locale)} ${t('schedulePoll.removeCandidate')}`}
                  className="choice-row flex h-7 items-center gap-1.5 pl-2.5 pr-2 text-footnote font-medium tabular-nums text-[var(--color-text-primary)]"
                >
                  <span className="text-[var(--color-text-secondary)]">
                    {start.toFormat('M/d ccc')}
                  </span>
                  {slotTimeRange(start, end)}
                  <svg
                    aria-hidden
                    width="10"
                    height="10"
                    viewBox="0 0 10 10"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    className="ml-0.5 text-[var(--color-text-tertiary)]"
                  >
                    <path d="m2.5 2.5 5 5m0-5-5 5" />
                  </svg>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function SlotCell({
  entry,
  showEnd,
  checked,
  disabled,
  onToggle,
}: {
  entry: GridEntry;
  showEnd: boolean;
  checked: boolean;
  disabled: boolean;
  onToggle: (candidate: SchedulePollCandidate) => void;
}) {
  const t = useT();
  const timezone = useUiStore((state) => state.timezone);
  const locale = useUiStore((state) => state.locale);
  const { candidate, start, end } = entry;
  const unavailable = !candidate.active || candidate.availability === 'hard';
  const soft = !unavailable && candidate.availability === 'soft';
  const note = unavailable
    ? t('schedulePoll.unavailable')
    : soft
      ? t('schedulePoll.availabilitySoft')
      : undefined;
  const label = formatSchedulePollSlotTime(candidate.startAt, candidate.endAt, timezone, locale);
  return (
    <label
      title={note ? `${slotTimeRange(start, end)} (${note})` : slotTimeRange(start, end)}
      data-availability={unavailable ? 'hard' : soft ? 'soft' : 'open'}
      className="slot-grid-cell flex h-6 cursor-pointer items-center justify-center gap-1 px-1.5 text-micro font-semibold tabular-nums"
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={unavailable || disabled}
        onChange={() => onToggle(candidate)}
        aria-label={note ? `${label} (${note})` : label}
        className="sr-only"
      />
      {(checked || showEnd) && (
        <span className="truncate">
          {showEnd ? slotTimeRange(start, end) : start.toLocaleString(DateTime.TIME_SIMPLE)}
        </span>
      )}
    </label>
  );
}

function Field({ label, ...input }: { label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="block min-w-0 text-footnote font-medium text-[var(--color-text-secondary)]">
      <span className="mb-1.5 block">{label}</span>
      <input {...input} className="input-modern input-compact w-full tabular-nums" />
    </label>
  );
}
