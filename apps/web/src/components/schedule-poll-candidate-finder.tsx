import { DateTime } from 'luxon';
import { useEffect, useRef, useState } from 'react';
import { formatSchedulePollSlotTime } from '@/components/schedule-poll-ui';
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

  return (
    <div className="rounded-2xl border border-[var(--color-border)] bg-[var(--color-surface-inset)] p-4 sm:p-5">
      <div className="mb-4">
        <h3 className="text-default font-semibold text-[var(--color-text-primary)]">
          {t('schedulePoll.findCandidates')}
        </h3>
        <p className="mt-1 text-footnote text-[var(--color-text-secondary)]">
          {t('schedulePoll.findCandidatesHint')}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <label className="text-footnote text-[var(--color-text-secondary)]">
          <span className="mb-1 block">{t('schedulePoll.fromDate')}</span>
          <input
            type="date"
            value={fromDate}
            onChange={(event) => setFromDate(event.target.value)}
            className="input-modern w-full"
          />
        </label>
        <label className="text-footnote text-[var(--color-text-secondary)]">
          <span className="mb-1 block">{t('schedulePoll.toDate')}</span>
          <input
            type="date"
            value={toDate}
            onChange={(event) => setToDate(event.target.value)}
            className="input-modern w-full"
          />
        </label>
        <label className="text-footnote text-[var(--color-text-secondary)]">
          <span className="mb-1 block">{t('schedulePoll.startTime')}</span>
          <input
            type="time"
            value={startTime}
            onChange={(event) => setStartTime(event.target.value)}
            className="input-modern w-full"
          />
        </label>
        <label className="text-footnote text-[var(--color-text-secondary)]">
          <span className="mb-1 block">{t('schedulePoll.endTime')}</span>
          <input
            type="time"
            value={endTime}
            onChange={(event) => setEndTime(event.target.value)}
            className="input-modern w-full"
          />
        </label>
        <label className="text-footnote text-[var(--color-text-secondary)]">
          <span className="mb-1 block">{t('schedulePoll.durationMinutes')}</span>
          <input
            type="number"
            min={15}
            max={1440}
            step={15}
            value={durationMinutes}
            onChange={(event) => setDurationMinutes(Number(event.target.value))}
            className="input-modern w-full"
          />
        </label>
        <button
          type="button"
          onClick={findCandidates}
          disabled={
            searching || memberIds.length === 0 || fromDate > toDate || durationMinutes < 15
          }
          className="btn-primary self-end px-4 text-default disabled:opacity-50"
        >
          {searching ? t('common.loading') : t('schedulePoll.findCandidates')}
        </button>
      </div>

      <div className="mt-4 border-t border-[var(--color-separator)] pt-4">
        <h4 className="text-footnote font-semibold text-[var(--color-text-primary)]">
          {t('schedulePoll.manualCandidate')}
        </h4>
        <p className="mb-3 mt-1 text-micro text-[var(--color-text-secondary)]">
          {t('schedulePoll.manualCandidateHint', { timezone })}
        </p>
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
          <label className="text-footnote text-[var(--color-text-secondary)]">
            <span className="mb-1 block">{t('schedulePoll.manualStartDate')}</span>
            <input
              type="date"
              value={manualStartDate}
              onChange={(event) => setManualStartDate(event.target.value)}
              className="input-modern w-full"
            />
          </label>
          <label className="text-footnote text-[var(--color-text-secondary)]">
            <span className="mb-1 block">{t('schedulePoll.manualStartTime')}</span>
            <input
              type="time"
              value={manualStartTime}
              onChange={(event) => setManualStartTime(event.target.value)}
              className="input-modern w-full"
            />
          </label>
          <label className="text-footnote text-[var(--color-text-secondary)]">
            <span className="mb-1 block">{t('schedulePoll.manualEndDate')}</span>
            <input
              type="date"
              value={manualEndDate}
              onChange={(event) => setManualEndDate(event.target.value)}
              className="input-modern w-full"
            />
          </label>
          <label className="text-footnote text-[var(--color-text-secondary)]">
            <span className="mb-1 block">{t('schedulePoll.manualEndTime')}</span>
            <input
              type="time"
              value={manualEndTime}
              onChange={(event) => setManualEndTime(event.target.value)}
              className="input-modern w-full"
            />
          </label>
          <button
            type="button"
            onClick={addManualCandidate}
            disabled={searching || memberIds.length === 0}
            className="btn-primary self-end px-4 text-default disabled:opacity-50"
          >
            {searching ? t('common.loading') : t('schedulePoll.addManualCandidate')}
          </button>
        </div>
      </div>

      {searched && (
        <div className="mt-4 border-t border-[var(--color-separator)] pt-3">
          {suggestions.length === 0 ? (
            <p className="py-2 text-body text-[var(--color-text-secondary)]">
              {t('schedulePoll.noCandidates')}
            </p>
          ) : (
            <ul className="divide-y divide-[var(--color-separator)]">
              {suggestions.map((candidate) => {
                const key = candidateKey(candidate);
                const unavailable = !candidate.active || candidate.availability === 'hard';
                const checked = selectedKeys.has(key);
                return (
                  <li key={key}>
                    <label className="flex min-h-12 items-center gap-3 py-2 text-body text-[var(--color-text-primary)]">
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={unavailable || searching}
                        onChange={() => toggle(candidate)}
                        className="h-4 w-4 accent-[var(--color-accent)]"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block font-medium">
                          {formatSchedulePollSlotTime(
                            candidate.startAt,
                            candidate.endAt,
                            timezone,
                            locale,
                          )}
                        </span>
                        <span className="block text-footnote text-[var(--color-text-secondary)]">
                          {unavailable
                            ? t('schedulePoll.unavailable')
                            : candidate.availability === 'soft'
                              ? t('schedulePoll.availabilitySoft')
                              : t('schedulePoll.availabilityOpen')}
                        </span>
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
          {selected.length > 0 && (
            <p className="mt-3 text-footnote font-medium text-[var(--color-accent)] tabular-nums">
              {t('schedulePoll.selectedCandidates', { count: selected.length })}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
