import { DateTime } from 'luxon';
import { type InputHTMLAttributes, useEffect, useRef, useState } from 'react';
import {
  formatSchedulePollSlotTime,
  SlotDayTile,
  slotTimeRange,
} from '@/components/schedule-poll-ui';
import { useT } from '@/i18n';
import { errorMessage } from '@/lib/api';
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
  const locale = useUiStore((state) => state.locale);
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
      for (const candidate of result.slots ?? []) merged.set(candidateKey(candidate), candidate);
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

  const days = (() => {
    const byDay = new Map<
      string,
      Array<{ candidate: SchedulePollCandidate; key: string; start: DateTime; end: DateTime }>
    >();
    for (const candidate of suggestions) {
      const start = DateTime.fromISO(candidate.startAt, { setZone: true })
        .setZone(timezone)
        .setLocale(locale);
      const end = DateTime.fromISO(candidate.endAt, { setZone: true })
        .setZone(timezone)
        .setLocale(locale);
      const day = start.toISODate() ?? candidate.startAt;
      const entry = { candidate, key: candidateKey(candidate), start, end };
      const group = byDay.get(day);
      if (group) group.push(entry);
      else byDay.set(day, [entry]);
    }
    return [...byDay.entries()];
  })();

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
            <>
              <p
                role="status"
                className={`mb-2.5 text-footnote font-semibold tabular-nums ${selected.length > 0 ? 'text-[var(--color-accent-ink)]' : 'text-[var(--color-text-tertiary)]'}`}
              >
                {t('schedulePoll.selectedCandidates', { count: selected.length })}
              </p>
              <div className="max-h-[440px] overflow-y-auto pr-1">
                <ul className="divide-y divide-[var(--color-separator)]">
                  {days.map(([day, group]) => (
                    <li key={day} className="flex gap-3 py-2.5 first:pt-0 last:pb-0">
                      <SlotDayTile date={group[0]?.start ?? DateTime.fromISO(day)} dense />
                      <ul className="flex min-w-0 flex-1 flex-wrap content-center gap-1.5">
                        {group.map(({ candidate, key, start, end }) => {
                          const unavailable =
                            !candidate.active || candidate.availability === 'hard';
                          const label = formatSchedulePollSlotTime(
                            candidate.startAt,
                            candidate.endAt,
                            timezone,
                            locale,
                          );
                          return (
                            <li key={key}>
                              <label
                                title={
                                  unavailable
                                    ? t('schedulePoll.unavailable')
                                    : candidate.availability === 'soft'
                                      ? t('schedulePoll.availabilitySoft')
                                      : undefined
                                }
                                className={`choice-row choice-chip flex h-8 cursor-pointer items-center gap-1.5 px-2.5 text-footnote font-medium tabular-nums ${unavailable ? 'text-[var(--color-text-tertiary)] line-through opacity-60' : 'text-[var(--color-text-primary)]'}`}
                              >
                                <input
                                  type="checkbox"
                                  checked={selectedKeys.has(key)}
                                  disabled={unavailable || searching}
                                  onChange={() => toggle(candidate)}
                                  aria-label={`${label}${
                                    unavailable
                                      ? ` (${t('schedulePoll.unavailable')})`
                                      : candidate.availability === 'soft'
                                        ? ` (${t('schedulePoll.availabilitySoft')})`
                                        : ''
                                  }`}
                                  className="sr-only"
                                />
                                {!unavailable && candidate.availability === 'soft' && (
                                  <span
                                    aria-hidden
                                    className="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--color-warning)]"
                                  />
                                )}
                                {slotTimeRange(start, end)}
                              </label>
                            </li>
                          );
                        })}
                      </ul>
                    </li>
                  ))}
                </ul>
              </div>
            </>
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

function Field({ label, ...input }: { label: string } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <label className="block min-w-0 text-footnote font-medium text-[var(--color-text-secondary)]">
      <span className="mb-1.5 block">{label}</span>
      <input {...input} className="input-modern input-compact w-full tabular-nums" />
    </label>
  );
}
