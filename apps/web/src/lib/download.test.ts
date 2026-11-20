import { afterEach, describe, expect, it, vi } from 'vitest';
import { startDownload } from './download';

describe('startDownload', () => {
  afterEach(() => vi.restoreAllMocks());

  it('clicks a same-tab anchor to the url and removes it', () => {
    const clicked: HTMLAnchorElement[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
      this: HTMLAnchorElement,
    ) {
      clicked.push(this);
      expect(document.body.contains(this)).toBe(true);
    });
    const open = vi.spyOn(window, 'open');

    startDownload('https://files.example/a.png?sig=1');

    expect(clicked).toHaveLength(1);
    expect(clicked[0]?.getAttribute('href')).toBe('https://files.example/a.png?sig=1');
    expect(clicked[0]?.hasAttribute('target')).toBe(false);
    expect(document.body.contains(clicked[0] ?? null)).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });
});
