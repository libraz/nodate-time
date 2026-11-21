import { useEffect, useRef, useState } from 'react';
import { Avatar } from '@/components/avatar';
import { RoleHint } from '@/components/role-hint';
import { useT } from '@/i18n';
import { client, errorMessage, isAbortError } from '@/lib/client';
import {
  DEFAULT_INVITE_ROLE,
  INVITE_ROLE_OPTIONS,
  type Role,
  roleLabelKey,
} from '@/lib/permissions';
import { toast } from '@/lib/toast';
import { useCalendarStore } from '@/stores/calendar-store';

/** Someone in the calendar's workspace who holds no grant on it yet. */
interface Candidate {
  id: string;
  name: string;
  email: string;
  avatarUrl?: string;
}

interface CandidatePage {
  items: Candidate[];
  hasMore: boolean;
}

const SEARCH_DEBOUNCE_MS = 250;

interface MemberAdderProps {
  calendarId: string;
  /** Leaves the picker; carries the ids that were added, empty when cancelled. */
  onDone: (addedIds: string[]) => void;
  /** Sends the reader to the invite links for people outside the workspace. */
  onInviteByLink: () => void;
}

/**
 * Picks people already in the workspace and adds them to a calendar in one go,
 * so colleagues do not each have to be sent a link. Key it by calendar: a
 * selection belongs to the calendar it was made for.
 */
export function MemberAdder({ calendarId, onDone, onInviteByLink }: MemberAdderProps) {
  const t = useT();
  const fetchMembers = useCalendarStore((s) => s.fetchMembers);
  const [query, setQuery] = useState('');
  const [role, setRole] = useState<Role>(DEFAULT_INVITE_ROLE);
  const [page, setPage] = useState<CandidatePage | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  // Kept by id with the person, so a choice survives a search that hides them.
  const [selected, setSelected] = useState<Map<string, Candidate>>(new Map());
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  // A touch screen would raise its keyboard over the list being picked from.
  useEffect(() => {
    if (window.matchMedia?.('(pointer: fine)').matches) searchRef.current?.focus();
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    const q = query.trim();
    const timer = setTimeout(
      async () => {
        try {
          const { data } = await client.GET('/calendars/{calendarId}/member-candidates', {
            params: { path: { calendarId }, query: { q } },
            signal: controller.signal,
          });
          setPage(data ? { items: data.items ?? [], hasMore: data.hasMore } : null);
          setFailed(null);
        } catch (e) {
          if (isAbortError(e)) return;
          setFailed(errorMessage(e));
        }
      },
      q ? SEARCH_DEBOUNCE_MS : 0,
    );
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [calendarId, query]);

  const toggle = (c: Candidate) =>
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(c.id)) next.delete(c.id);
      else next.set(c.id, c);
      return next;
    });

  const busy = progress !== null;

  const submit = async () => {
    const people = [...selected.values()];
    const added: string[] = [];
    let firstError: string | null = null;
    setProgress({ done: 0, total: people.length });
    // One at a time: each grant takes the calendar's lock on the server anyway.
    for (const person of people) {
      try {
        await client.POST('/calendars/{calendarId}/members', {
          params: { path: { calendarId } },
          // The picker only offers the two roles a grant may carry.
          body: { userId: person.id, role: role as 'editor' | 'viewer' },
        });
        added.push(person.id);
      } catch (e) {
        firstError ??= errorMessage(e);
      }
      setProgress({ done: added.length, total: people.length });
    }
    setProgress(null);
    if (added.length > 0) await fetchMembers(calendarId);

    if (firstError === null) {
      toast.success(t('members.addedMany', { count: added.length }));
      onDone(added);
      return;
    }
    // Keep only who is still missing, so pressing again retries just them.
    setSelected((prev) => {
      const next = new Map(prev);
      for (const id of added) next.delete(id);
      return next;
    });
    setPage((p) => p && { ...p, items: p.items.filter((c) => !added.includes(c.id)) });
    toast.error(
      t('members.addPartial', { count: people.length - added.length, reason: firstError }),
    );
  };

  const items = page?.items ?? [];
  const count = selected.size;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="space-y-3 border-b border-[var(--color-border)] px-5 pt-3 pb-4">
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => onDone([])}
            aria-label={t('members.backToList')}
            title={t('members.backToList')}
            className="icon-btn -ml-2 h-8 w-8"
          >
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </button>
          <h3 className="text-default font-semibold text-[var(--color-text-primary)]">
            {t('members.addSection')}
          </h3>
        </div>
        <div className="relative">
          <svg
            className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-[var(--color-text-tertiary)]"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="M20 20l-3.5-3.5" />
          </svg>
          <input
            ref={searchRef}
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('members.addSearch')}
            aria-label={t('members.addSearch')}
            maxLength={100}
            className="input-modern w-full py-2 pr-3 pl-9 text-body"
          />
        </div>
        {count > 0 && (
          <ul className="flex flex-wrap gap-1.5" aria-label={t('members.addChosen')}>
            {[...selected.values()].map((c) => (
              <li key={c.id} className="reveal-in">
                <button
                  type="button"
                  onClick={() => toggle(c)}
                  disabled={busy}
                  aria-label={t('members.addUnselect', { name: c.name || c.email })}
                  className="inline-flex h-7 max-w-[180px] items-center gap-1.5 rounded-full bg-[var(--color-accent-bg)] pr-2 pl-0.5 text-footnote font-medium text-[var(--color-accent-ink)] hover:brightness-95 disabled:opacity-60"
                >
                  <Avatar src={c.avatarUrl} name={c.name || c.email} size="h-6 w-6" round />
                  <span className="truncate">{c.name || c.email}</span>
                  <svg
                    width="10"
                    height="10"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="3"
                    strokeLinecap="round"
                    aria-hidden="true"
                    className="shrink-0 opacity-70"
                  >
                    <path d="M18 6L6 18M6 6l12 12" />
                  </svg>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
        {failed ? (
          <p className="mx-2 my-3 rounded-[var(--radius-sm)] bg-[var(--color-danger-bg)] px-3 py-2 text-caption text-[var(--color-danger)]">
            {failed}
          </p>
        ) : !page ? (
          <p className="px-2 py-6 text-center text-caption text-[var(--color-text-tertiary)]">
            {t('common.loading')}
          </p>
        ) : items.length === 0 ? (
          <p className="px-4 py-10 text-center text-body text-[var(--color-text-secondary)]">
            {query.trim() ? t('members.addNoMatch') : t('members.addNobody')}
          </p>
        ) : (
          <ul className="space-y-0.5">
            {items.map((c) => {
              const on = selected.has(c.id);
              return (
                <li key={c.id}>
                  <label
                    className={`flex cursor-pointer items-center gap-3 rounded-[var(--radius-control)] px-2 py-2 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-[var(--color-accent)] ${
                      on ? 'bg-[var(--color-accent-bg)]' : 'hover:bg-[var(--color-hover)]'
                    }`}
                  >
                    <input
                      type="checkbox"
                      className="sr-only"
                      checked={on}
                      disabled={busy}
                      onChange={() => toggle(c)}
                    />
                    <Avatar src={c.avatarUrl} name={c.name || c.email} size="h-9 w-9" round />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-body font-medium text-[var(--color-text-primary)]">
                        {c.name || c.email}
                      </span>
                      {c.name && (
                        <span className="truncate text-caption text-[var(--color-text-tertiary)]">
                          {c.email}
                        </span>
                      )}
                    </span>
                    <span
                      aria-hidden
                      className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-[1.5px] transition-colors ${
                        on
                          ? 'border-[var(--color-accent)] bg-[var(--color-accent)] text-[var(--color-text-on-accent)]'
                          : 'border-[var(--color-border-strong)]'
                      }`}
                    >
                      {on && (
                        <svg
                          className="pop-in"
                          width="11"
                          height="11"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="3.5"
                          strokeLinecap="round"
                          strokeLinejoin="round"
                        >
                          <path d="M5 12.5l4.5 4.5L19 7.5" />
                        </svg>
                      )}
                    </span>
                  </label>
                </li>
              );
            })}
          </ul>
        )}
        {page?.hasMore && (
          <p className="px-2 py-3 text-center text-caption text-[var(--color-text-tertiary)]">
            {t('members.addMore')}
          </p>
        )}
      </div>

      <div className="space-y-3 border-t border-[var(--color-border)] px-5 pt-3 pb-4">
        <div className="space-y-1.5">
          <div
            className="segmented-control w-full"
            role="radiogroup"
            aria-label={t('members.role')}
          >
            {INVITE_ROLE_OPTIONS.map((r) => (
              // biome-ignore lint/a11y/useSemanticElements: the segmented control styles buttons, not radio inputs
              <button
                key={r}
                type="button"
                role="radio"
                aria-checked={role === r}
                data-active={role === r}
                disabled={busy}
                onClick={() => setRole(r)}
              >
                {t(roleLabelKey(r))}
              </button>
            ))}
          </div>
          <RoleHint role={role} />
        </div>
        <button
          type="button"
          onClick={submit}
          disabled={count === 0 || busy}
          className="btn-primary h-10 w-full text-body font-semibold tabular-nums disabled:opacity-50"
        >
          {progress
            ? t('members.addingProgress', { done: progress.done, total: progress.total })
            : count === 0
              ? t('members.addPick')
              : t('members.addSelected', { count })}
        </button>
        <p className="text-center text-caption text-[var(--color-text-tertiary)]">
          {t('members.addOutside')}{' '}
          <button
            type="button"
            onClick={onInviteByLink}
            className="font-medium text-[var(--color-accent-ink)] hover:underline"
          >
            {t('members.addViaLink')}
          </button>
        </p>
      </div>
    </div>
  );
}
