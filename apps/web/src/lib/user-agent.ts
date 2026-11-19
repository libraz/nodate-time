export interface DeviceSummary {
  /** Browser or client name, e.g. "Chrome" or "curl". Empty when unrecognised. */
  browser: string;
  /** Operating system, e.g. "macOS". Empty when the string does not say. */
  os: string;
  mobile: boolean;
}

const BROWSERS: [RegExp, string][] = [
  [/\bEdg(?:e|A|iOS)?\//, 'Edge'],
  [/\bOPR\//, 'Opera'],
  [/\b(?:Firefox|FxiOS)\//, 'Firefox'],
  [/\b(?:HeadlessChrome|Chrome|CriOS)\//, 'Chrome'],
  [/\bVersion\/[\d.]+.*\bSafari\//, 'Safari'],
];

const SYSTEMS: [RegExp, string][] = [
  [/\biPad\b/, 'iPadOS'],
  [/\biPhone\b/, 'iOS'],
  [/\bAndroid\b/, 'Android'],
  [/\bCrOS\b/, 'ChromeOS'],
  [/\bMac OS X\b/, 'macOS'],
  [/\bWindows\b/, 'Windows'],
  [/\bLinux\b/, 'Linux'],
];

/**
 * Reduces a User-Agent header to what a person recognises their own device
 * by. A non-browser client falls back to its leading product token.
 */
export function describeUserAgent(ua: string): DeviceSummary {
  const browser =
    BROWSERS.find(([re]) => re.test(ua))?.[1] ??
    (/^Mozilla\//.test(ua) ? '' : (ua.match(/^([A-Za-z][\w.-]*)\//)?.[1] ?? ''));
  const os = SYSTEMS.find(([re]) => re.test(ua))?.[1] ?? '';
  return { browser, os, mobile: /\b(?:Mobile|iPhone|iPad|Android)\b/.test(ua) };
}
