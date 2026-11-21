import { afterEach, describe, expect, it, vi } from 'vitest';
import { hasNewBuild, parseEntryScript } from './build-check';

const INDEX = `<!DOCTYPE html>
<html lang="ja">
  <head>
    <script>(() => { const x = 1; })();</script>
    <script type="module" crossorigin src="/assets/index-BIVFl_Fr.js"></script>
    <link rel="stylesheet" crossorigin href="/assets/index-abc.css">
  </head>
  <body><div id="root"></div></body>
</html>`;

afterEach(() => vi.unstubAllGlobals());

describe('parseEntryScript', () => {
  it('picks the module entry script', () => {
    expect(parseEntryScript(INDEX)).toBe('/assets/index-BIVFl_Fr.js');
  });

  it('returns null when there is none', () => {
    expect(parseEntryScript('<html></html>')).toBeNull();
  });
});

describe('hasNewBuild', () => {
  const stubFetch = (impl: () => Promise<unknown>) => vi.stubGlobal('fetch', vi.fn(impl));

  it('reports a differing entry script', async () => {
    stubFetch(async () => ({ ok: true, text: async () => INDEX }));
    expect(await hasNewBuild('/assets/index-OLD.js')).toBe(true);
  });

  it('reports no change for the same script', async () => {
    stubFetch(async () => ({ ok: true, text: async () => INDEX }));
    expect(await hasNewBuild('/assets/index-BIVFl_Fr.js')).toBe(false);
  });

  it('treats a rejected fetch as no new build', async () => {
    stubFetch(async () => {
      throw new Error('offline');
    });
    expect(await hasNewBuild('/assets/index-OLD.js')).toBe(false);
  });

  it('treats a non-OK response as no new build', async () => {
    stubFetch(async () => ({ ok: false, text: async () => INDEX }));
    expect(await hasNewBuild('/assets/index-OLD.js')).toBe(false);
  });

  it('treats an unparsable response as no new build', async () => {
    stubFetch(async () => ({ ok: true, text: async () => 'nope' }));
    expect(await hasNewBuild('/assets/index-OLD.js')).toBe(false);
  });
});
