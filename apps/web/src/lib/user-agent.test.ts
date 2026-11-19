import { describe, expect, it } from 'vitest';
import { describeUserAgent } from './user-agent';

const CHROME_MAC =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36';

describe('describeUserAgent', () => {
  it.each([
    [CHROME_MAC, 'Chrome', 'macOS', false],
    [CHROME_MAC.replace('Chrome/', 'HeadlessChrome/'), 'Chrome', 'macOS', false],
    [`${CHROME_MAC} Edg/130.0.0.0`, 'Edge', 'macOS', false],
    [
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
      'Safari',
      'iOS',
      true,
    ],
    [
      'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36',
      'Chrome',
      'Android',
      true,
    ],
    [
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0',
      'Firefox',
      'Windows',
      false,
    ],
    ['curl/8.7.1', 'curl', '', false],
    ['Mozilla/5.0 (compatible)', '', '', false],
  ])('%s', (ua, browser, os, mobile) => {
    expect(describeUserAgent(ua)).toEqual({ browser, os, mobile });
  });
});
