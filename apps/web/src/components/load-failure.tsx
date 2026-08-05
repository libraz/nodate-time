import { useState } from 'react';
import { useT } from '@/i18n';

interface LoadFailureProps {
  /** What could not be loaded, in the reader's language. */
  title: string;
  /** The failure's own message, when there is one worth showing. */
  detail?: string | null;
  onRetry: () => void | Promise<void>;
}

/**
 * Runs a retry and reports whether one is in flight.
 *
 * The button has to go quiet while the request is out: without that, a slow
 * network invites a second and third press, and each one starts another round
 * of the same fetches.
 */
function useRetry(onRetry: () => void | Promise<void>): [boolean, () => void] {
  const [retrying, setRetrying] = useState(false);
  const run = () => {
    if (retrying) return;
    setRetrying(true);
    Promise.resolve(onRetry()).finally(() => setRetrying(false));
  };
  return [retrying, run];
}

/**
 * A screen-filling stand-in for content that could not be loaded.
 *
 * Used where the alternative is an empty view: an empty calendar grid and an
 * account with no calendars in it look identical, so a failure that leaves
 * one behind reads as an answer rather than as a missing one.
 */
export function LoadFailure({ title, detail, onRetry }: LoadFailureProps) {
  const t = useT();
  const [retrying, run] = useRetry(onRetry);

  return (
    <div className="flex h-full flex-1 items-center justify-center p-6">
      <div className="max-w-sm text-center">
        <p className="text-subhead font-medium text-[var(--color-text-primary)]">{title}</p>
        {detail && <p className="mt-2 text-body text-[var(--color-text-secondary)]">{detail}</p>}
        <button
          type="button"
          onClick={run}
          disabled={retrying}
          className="mt-4 rounded-full bg-[var(--color-accent-bg)] px-4 py-2 text-default font-medium text-[var(--color-accent)] disabled:opacity-50"
        >
          {retrying ? t('common.loading') : t('error.retry')}
        </button>
      </div>
    </div>
  );
}

/**
 * An inline strip for a partial failure -- the content is usable, but some of
 * it is missing and the gap changes what the user is allowed to do.
 */
export function LoadFailureBanner({ title, onRetry }: Omit<LoadFailureProps, 'detail'>) {
  const t = useT();
  const [retrying, run] = useRetry(onRetry);

  return (
    <div className="flex items-center justify-between gap-3 bg-[var(--color-danger-bg)] px-4 py-2 text-footnote text-[var(--color-danger)]">
      <span>{title}</span>
      <button
        type="button"
        onClick={run}
        disabled={retrying}
        className="shrink-0 font-medium underline disabled:opacity-50"
      >
        {retrying ? t('common.loading') : t('error.retry')}
      </button>
    </div>
  );
}
