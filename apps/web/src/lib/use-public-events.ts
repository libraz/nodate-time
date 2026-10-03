import type { DateTime } from 'luxon';
import { useCallback, useEffect, useState } from 'react';
import { api, isAbortError } from '@/lib/api';
import { gridRange } from '@/lib/date-utils';
import { type PublicEvent, type ShareFailure, shareFailureOf } from '@/lib/public-calendar';

export interface PublicEvents {
  /** The events read for the month and zone on screen; never another month's. */
  events: PublicEvent[];
  failure: ShareFailure | null;
  retry: () => void;
}

/**
 * The events a published link shows for one month, read in the viewer's zone.
 *
 * A request the page has moved past is cancelled, and its answer could not be
 * drawn anyway: what is held is keyed by the range it was read for. A failure
 * is reported as one rather than drawn as a month with nothing in it.
 */
export function usePublicEvents(
  token: string,
  enabled: boolean,
  month: DateTime,
  timezone: string,
): PublicEvents {
  // The grid draws part of the neighbouring months, and the dates are days in
  // the zone this page renders -- both have to be said, or the edge cells stay
  // empty and an event near a month boundary lands in the wrong one.
  const { start, end } = gridRange(month, timezone);
  const path = `/share/${token}/events?start=${start}&end=${end}&tz=${encodeURIComponent(timezone)}`;
  const [loaded, setLoaded] = useState<{ path: string; events: PublicEvent[] }>({
    path: '',
    events: [],
  });
  const [failure, setFailure] = useState<ShareFailure | null>(null);
  const [attempt, setAttempt] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: `attempt` is how a retry asks again
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    setFailure(null);
    api
      .get<PublicEvent[]>(path, true, controller.signal)
      .then((events) => {
        if (!controller.signal.aborted) setLoaded({ path, events });
      })
      .catch((e: unknown) => {
        if (!controller.signal.aborted && !isAbortError(e)) setFailure(shareFailureOf(e));
      });
    return () => controller.abort();
  }, [enabled, path, attempt]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { events: loaded.path === path ? loaded.events : [], failure, retry };
}
