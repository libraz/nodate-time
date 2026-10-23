import { describe, expect, it } from 'vitest';
import { isSafeRedirect, safeRedirect } from './safe-redirect';

describe('safeRedirect', () => {
  it('keeps same-origin paths', () => {
    expect(safeRedirect('/calendar?x=1#h')).toBe('/calendar?x=1#h');
  });

  it.each([
    ['protocol-relative', '//evil.com'],
    ['backslash', '/\\evil.com'],
    ['line feed', '/a\nb'],
    ['carriage return', '/a\rb'],
    ['absolute URL', 'https://evil.com'],
    ['relative', 'calendar'],
    ['over length', `/${'a'.repeat(512)}`],
  ])('falls back to / for %s', (_, value) => {
    expect(isSafeRedirect(value)).toBe(false);
    expect(safeRedirect(value)).toBe('/');
  });

  it('counts characters rather than UTF-16 units', () => {
    expect(isSafeRedirect(`/${'😀'.repeat(511)}`)).toBe(true);
    expect(isSafeRedirect(`/${'😀'.repeat(512)}`)).toBe(false);
  });

  it('falls back to / when absent', () => {
    expect(safeRedirect(undefined)).toBe('/');
    expect(safeRedirect('')).toBe('/');
  });
});
