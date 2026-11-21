import { create } from 'zustand';

const INDEX_URL = '/index.html';
const CHECK_INTERVAL_MS = 30 * 60 * 1000;
const RELOAD_KEY = 'tt_preload_reload_at';
const RELOAD_GUARD_MS = 30_000;

interface BuildState {
  /** True once the server's index.html names a different entry script. */
  hasNewBuild: boolean;
}

export const useBuildStore = create<BuildState>(() => ({ hasNewBuild: false }));

/** Extracts the hashed entry module's src from an index.html document, or null. */
export function parseEntryScript(html: string): string | null {
  for (const tag of html.match(/<script\b[^>]*>/gi) ?? []) {
    if (!/\btype=["']module["']/i.test(tag)) continue;
    const src = /\bsrc=["']([^"']+)["']/i.exec(tag);
    if (src?.[1]) return src[1];
  }
  return null;
}

/** The entry script the running page was loaded with, or null when it has none (dev server). */
export function runningEntryScript(doc: Document = document): string | null {
  const el = doc.querySelector<HTMLScriptElement>('script[type="module"][src]');
  return el?.getAttribute('src') ?? null;
}

/** Whether the deployed index.html names a different entry script. Failures mean "no". */
export async function hasNewBuild(running: string | null): Promise<boolean> {
  if (!running) return false;
  try {
    const res = await fetch(INDEX_URL, { cache: 'no-store' });
    if (!res.ok) return false;
    const latest = parseEntryScript(await res.text());
    return latest !== null && latest !== running;
  } catch {
    return false;
  }
}

/** Starts stale-build detection and the preload-error recovery; call once at startup. */
export function startBuildCheck(): void {
  window.addEventListener('vite:preloadError', (event) => {
    const last = Number(sessionStorage.getItem(RELOAD_KEY) ?? 0);
    if (Date.now() - last < RELOAD_GUARD_MS) return;
    sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
    event.preventDefault();
    location.reload();
  });

  if (import.meta.env.DEV) return;
  const running = runningEntryScript();
  if (!running) return;

  let timer: ReturnType<typeof setInterval> | undefined;
  const stop = () => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', onVisible);
  };
  const check = async () => {
    if (!(await hasNewBuild(running))) return;
    useBuildStore.setState({ hasNewBuild: true });
    stop();
  };
  const arm = () => {
    clearInterval(timer);
    timer = setInterval(check, CHECK_INTERVAL_MS);
  };
  function onVisible() {
    if (document.visibilityState === 'visible') {
      void check();
      arm();
    } else {
      clearInterval(timer);
    }
  }
  document.addEventListener('visibilitychange', onVisible);
  if (document.visibilityState === 'visible') arm();
}
